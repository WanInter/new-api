package common

import (
	"fmt"
	"strconv"
	"strings"

	commonutil "github.com/QuantumNous/new-api/common"
)

// VideoOutputSpec is the normalized public output request. Size remains for
// legacy provider compatibility, while aspect_ratio and resolution are the
// canonical cross-channel fields.
type VideoOutputSpec struct {
	Size        string `json:"size,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	PixelWidth  int    `json:"-"`
	PixelHeight int    `json:"-"`
}

// MultipartVideoOutputOptions controls wire-compatibility behavior while
// rewriting a multipart request to its normalized video output fields.
//
// Some Sora-compatible providers still consume ratio or aspectRatio. Those
// aliases can be kept only when they were present in the original form, with
// their values updated to match aspect_ratio.
type MultipartVideoOutputOptions struct {
	PreserveAspectRatioAliases bool
}

func (s VideoOutputSpec) HasPixelSize() bool {
	return s.PixelWidth > 0 && s.PixelHeight > 0
}

// EffectiveResolution returns only an explicit quality tier. A pixel size is
// not globally convertible to a provider resolution label; that mapping is a
// model capability concern.
func (s VideoOutputSpec) EffectiveResolution() string {
	return s.Resolution
}

// NormalizeTaskSubmitVideoOutput canonicalizes the public video output
// fields and rejects ambiguous requests before an adaptor can apply a
// channel-specific fallback. It deliberately leaves non-WxH legacy size
// values intact for providers that define their own size syntax.
func NormalizeTaskSubmitVideoOutput(req *TaskSubmitReq) (*VideoOutputSpec, error) {
	if req == nil {
		return nil, fmt.Errorf("video request is required")
	}

	topLevelAspectRatio := hasTopLevelVideoAspectRatio(req)
	aspectRatio, err := normalizeTaskAspectRatio(req)
	if err != nil {
		return nil, err
	}
	resolution, err := normalizeTaskResolution(req)
	if err != nil {
		return nil, err
	}
	size, width, height, pixelSize, err := normalizeTaskSize(req)
	if err != nil {
		return nil, err
	}

	spec := &VideoOutputSpec{
		Size:        size,
		AspectRatio: aspectRatio,
		Resolution:  resolution,
	}
	if pixelSize {
		spec.PixelWidth = width
		spec.PixelHeight = height
		sizeRatio := aspectRatioFromDimensions(width, height)
		if spec.AspectRatio != "" && spec.AspectRatio != "adaptive" && spec.AspectRatio != sizeRatio {
			return nil, fmt.Errorf("size %q conflicts with aspect_ratio %q", size, spec.AspectRatio)
		}
		if spec.AspectRatio == "adaptive" {
			return nil, fmt.Errorf("size %q conflicts with adaptive aspect_ratio", size)
		}
		if spec.AspectRatio == "" {
			spec.AspectRatio = sizeRatio
		}
	}

	req.Size = spec.Size
	req.AspectRatio = spec.AspectRatio
	req.Ratio = ""
	req.AspectRatioAlias = ""
	req.Resolution = spec.Resolution
	req.VideoOutput = spec
	normalizeTaskOutputMetadata(req, spec, topLevelAspectRatio)
	return spec, nil
}

// ApplyNormalizedTaskMultipartVideoOutput writes the canonical video output
// fields into a parsed multipart form. When metadata was submitted as a JSON
// string, it is re-serialized from the normalized request so nested aliases
// cannot contradict the top-level output fields.
func ApplyNormalizedTaskMultipartVideoOutput(values map[string][]string, req TaskSubmitReq, options MultipartVideoOutputOptions) error {
	if values == nil {
		return nil
	}

	_, hadRatio := values["ratio"]
	_, hadAspectRatioAlias := values["aspectRatio"]
	delete(values, "ratio")
	delete(values, "aspectRatio")

	setMultipartVideoOutputValue(values, "size", req.Size)
	setMultipartVideoOutputValue(values, "aspect_ratio", req.AspectRatio)
	setMultipartVideoOutputValue(values, "resolution", req.Resolution)

	if options.PreserveAspectRatioAliases && req.AspectRatio != "" {
		if hadRatio {
			values["ratio"] = []string{req.AspectRatio}
		}
		if hadAspectRatioAlias {
			values["aspectRatio"] = []string{req.AspectRatio}
		}
	}

	if _, hasMetadata := values["metadata"]; !hasMetadata || req.Metadata == nil {
		return nil
	}
	metadata, err := commonutil.Marshal(req.Metadata)
	if err != nil {
		return fmt.Errorf("marshal normalized metadata: %w", err)
	}
	values["metadata"] = []string{string(metadata)}
	return nil
}

func setMultipartVideoOutputValue(values map[string][]string, key, value string) {
	if value == "" {
		delete(values, key)
		return
	}
	values[key] = []string{value}
}

func normalizeTaskAspectRatio(req *TaskSubmitReq) (string, error) {
	values := []struct {
		name  string
		value string
	}{
		{name: "aspect_ratio", value: req.AspectRatio},
		{name: "ratio", value: req.Ratio},
		{name: "aspectRatio", value: req.AspectRatioAlias},
	}

	canonical := ""
	for _, item := range values {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		normalized, err := NormalizeVideoAspectRatio(item.value)
		if err != nil {
			return "", fmt.Errorf("%s: %w", item.name, err)
		}
		if canonical != "" && canonical != normalized {
			return "", fmt.Errorf("%s %q conflicts with aspect_ratio %q", item.name, normalized, canonical)
		}
		canonical = normalized
	}
	if canonical != "" {
		return canonical, nil
	}
	// A concrete top-level size determines its own aspect ratio. Metadata is a
	// compatibility fallback, so it must not contradict or invalidate that
	// explicit public field. Legacy provider-specific size strings still allow
	// metadata.aspect_ratio as a fallback.
	if hasPixelSize, err := hasTopLevelVideoPixelSize(req); err != nil {
		return "", err
	} else if hasPixelSize {
		return "", nil
	}
	for _, key := range []string{"aspect_ratio", "ratio", "aspectRatio"} {
		value, ok, err := taskMetadataString(req.Metadata, key)
		if err != nil {
			return "", err
		}
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		normalized, err := NormalizeVideoAspectRatio(value)
		if err != nil {
			return "", fmt.Errorf("metadata.%s: %w", key, err)
		}
		if canonical != "" && canonical != normalized {
			return "", fmt.Errorf("metadata.%s %q conflicts with aspect_ratio %q", key, normalized, canonical)
		}
		canonical = normalized
	}
	return canonical, nil
}

func normalizeTaskResolution(req *TaskSubmitReq) (string, error) {
	values := []struct {
		name  string
		value string
	}{
		{name: "resolution", value: req.Resolution},
	}

	canonical := ""
	for _, item := range values {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		normalized, err := NormalizeVideoOutputResolution(item.value)
		if err != nil {
			return "", fmt.Errorf("%s: %w", item.name, err)
		}
		if canonical != "" && canonical != normalized {
			return "", fmt.Errorf("%s %q conflicts with resolution %q", item.name, normalized, canonical)
		}
		canonical = normalized
	}
	if canonical != "" {
		return canonical, nil
	}
	value, ok, err := taskMetadataString(req.Metadata, "resolution")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		return "", nil
	}
	normalized, err := NormalizeVideoOutputResolution(value)
	if err != nil {
		return "", fmt.Errorf("metadata.resolution: %w", err)
	}
	return normalized, nil
}

func normalizeTaskSize(req *TaskSubmitReq) (string, int, int, bool, error) {
	value := strings.TrimSpace(req.Size)
	if value == "" && !hasTopLevelVideoAspectRatio(req) {
		if metadataSize, ok, err := taskMetadataString(req.Metadata, "size"); err != nil {
			return "", 0, 0, false, err
		} else if ok {
			value = strings.TrimSpace(metadataSize)
		}
	}
	if value == "" {
		return "", 0, 0, false, nil
	}

	normalized, width, height, isPixelSize, err := NormalizeVideoPixelSize(value)
	if err != nil {
		return "", 0, 0, false, err
	}
	if !isPixelSize {
		return strings.TrimSpace(value), 0, 0, false, nil
	}
	return normalized, width, height, true, nil
}

// NormalizeVideoAspectRatio validates and reduces a W:H ratio. adaptive is a
// supported provider-level mode and cannot be combined with a concrete size.
func NormalizeVideoAspectRatio(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "adaptive" {
		return value, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("aspect ratio must use W:H format")
	}
	width, errWidth := parsePositiveVideoDimension(parts[0])
	height, errHeight := parsePositiveVideoDimension(parts[1])
	if errWidth != nil || errHeight != nil {
		return "", fmt.Errorf("aspect ratio must use positive integer dimensions")
	}
	return aspectRatioFromDimensions(width, height), nil
}

// NormalizeVideoOutputResolution accepts a provider quality label but does
// not decide whether the selected model supports it. Capability matching owns
// that decision. 2160p is the canonical 4k alias.
func NormalizeVideoOutputResolution(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "2160p" || value == "4k" {
		return "4k", nil
	}
	if !strings.HasSuffix(value, "p") {
		return "", fmt.Errorf("resolution must be a quality label such as 720p or 4k")
	}
	resolution, err := parsePositiveVideoDimension(strings.TrimSuffix(value, "p"))
	if err != nil {
		return "", fmt.Errorf("resolution must be a quality label such as 720p or 4k")
	}
	return strconv.Itoa(resolution) + "p", nil
}

// NormalizeVideoPixelSize recognizes an exact WxH value. Other legacy size
// syntaxes (for example 1920*1080 or 720p) are intentionally passed through.
func NormalizeVideoPixelSize(value string) (string, int, int, bool, error) {
	value = strings.TrimSpace(value)
	canonical := strings.NewReplacer("X", "x", "×", "x").Replace(value)
	parts := strings.Split(canonical, "x")
	if len(parts) != 2 {
		return value, 0, 0, false, nil
	}
	width, errWidth := parsePositiveVideoDimension(parts[0])
	height, errHeight := parsePositiveVideoDimension(parts[1])
	if errWidth != nil || errHeight != nil {
		return "", 0, 0, false, fmt.Errorf("size must use positive integer WxH dimensions")
	}
	return strconv.Itoa(width) + "x" + strconv.Itoa(height), width, height, true, nil
}

func normalizeTaskOutputMetadata(req *TaskSubmitReq, spec *VideoOutputSpec, topLevelAspectRatio bool) {
	if req.Metadata == nil || spec == nil {
		return
	}
	if spec.Size != "" {
		req.Metadata["size"] = spec.Size
	} else if topLevelAspectRatio {
		delete(req.Metadata, "size")
	}
	// ratio and aspectRatio are input-only compatibility aliases. Leaving an
	// old value here lets adaptors that forward metadata choose it over the
	// canonical top-level value.
	delete(req.Metadata, "ratio")
	delete(req.Metadata, "aspectRatio")
	delete(req.Metadata, "aspect_ratio")
	if spec.AspectRatio != "" {
		req.Metadata["aspect_ratio"] = spec.AspectRatio
	}
	if spec.Resolution != "" {
		req.Metadata["resolution"] = spec.Resolution
	}
}

func hasTopLevelVideoAspectRatio(req *TaskSubmitReq) bool {
	return strings.TrimSpace(req.AspectRatio) != "" ||
		strings.TrimSpace(req.Ratio) != "" ||
		strings.TrimSpace(req.AspectRatioAlias) != ""
}

func hasTopLevelVideoPixelSize(req *TaskSubmitReq) (bool, error) {
	if strings.TrimSpace(req.Size) == "" {
		return false, nil
	}
	_, _, _, isPixelSize, err := NormalizeVideoPixelSize(req.Size)
	return isPixelSize, err
}

func taskMetadataString(metadata map[string]interface{}, key string) (string, bool, error) {
	if metadata == nil {
		return "", false, nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return "", false, nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("metadata.%s must be a string", key)
	}
	return stringValue, true, nil
}

func parsePositiveVideoDimension(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 || parsed > 32768 {
		return 0, fmt.Errorf("invalid dimension")
	}
	return parsed, nil
}

func aspectRatioFromDimensions(width, height int) string {
	divisor := greatestCommonDivisor(width, height)
	return strconv.Itoa(width/divisor) + ":" + strconv.Itoa(height/divisor)
}

func greatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}
