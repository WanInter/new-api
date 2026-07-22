package gemini

import (
	"fmt"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const defaultVeoResolution = "720p"

// VeoOutput describes the explicitly selected output fields that can be sent
// to the Veo API. An omitted resolution intentionally remains omitted so the
// provider can apply its own default.
type VeoOutput struct {
	AspectRatio string
	Resolution  string
}

type veoLegacySizeOutput struct {
	AspectRatio string
	Resolution  string
}

// veoLegacySizeOutputs retains the exact pixel forms that older clients used
// with this adapter. Veo accepts quality labels and aspect ratios, not an
// arbitrary output geometry, so never derive a label from an unknown WxH.
var veoLegacySizeOutputs = map[string]veoLegacySizeOutput{
	"1280x720":  {AspectRatio: "16:9", Resolution: "720p"},
	"720x1280":  {AspectRatio: "9:16", Resolution: "720p"},
	"1920x1080": {AspectRatio: "16:9", Resolution: "1080p"},
	"1080x1920": {AspectRatio: "9:16", Resolution: "1080p"},
	"3840x2160": {AspectRatio: "16:9", Resolution: "4k"},
	"2160x3840": {AspectRatio: "9:16", Resolution: "4k"},
}

// ParseVeoDurationSeconds extracts durationSeconds from metadata.
// Returns 8 (Veo default) when not specified or invalid.
func ParseVeoDurationSeconds(metadata map[string]any) int {
	if metadata == nil {
		return 8
	}
	v, ok := metadata["durationSeconds"]
	if !ok {
		return 8
	}
	switch n := v.(type) {
	case float64:
		if int(n) > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return 8
}

// ParseVeoResolution extracts resolution from metadata.
// Returns "720p" when not specified.
func ParseVeoResolution(metadata map[string]any) string {
	if metadata == nil {
		return defaultVeoResolution
	}
	v, ok := metadata["resolution"]
	if !ok {
		return defaultVeoResolution
	}
	if s, ok := v.(string); ok && s != "" {
		return strings.ToLower(s)
	}
	return defaultVeoResolution
}

// ResolveVeoDuration returns the effective duration in seconds.
// Priority: metadata["durationSeconds"] > stdDuration > stdSeconds > default (8).
func ResolveVeoDuration(metadata map[string]any, stdDuration int, stdSeconds string) int {
	if metadata != nil {
		if _, exists := metadata["durationSeconds"]; exists {
			if d := ParseVeoDurationSeconds(metadata); d > 0 {
				return d
			}
		}
	}
	if stdDuration > 0 {
		return stdDuration
	}
	if s, err := strconv.Atoi(stdSeconds); err == nil && s > 0 {
		return s
	}
	return 8
}

// ResolveVeoResolution returns the effective resolution string (lowercase).
// An exact registered legacy size may select a resolution; an arbitrary pixel
// size is never guessed and falls back to the provider default here. Normal
// relay flow rejects that unsupported size before this billing helper runs.
func ResolveVeoResolution(metadata map[string]any, stdSize string) string {
	return ResolveVeoRequestResolution("", metadata, stdSize)
}

// ResolveVeoRequestResolution gives the public video resolution field
// precedence over metadata and the legacy size-derived fallback.
func ResolveVeoRequestResolution(requestResolution string, metadata map[string]any, stdSize string) string {
	output, err := ResolveVeoRequestOutput(&relaycommon.TaskSubmitReq{
		Resolution: requestResolution,
		Metadata:   metadata,
		Size:       stdSize,
	}, "")
	if err == nil && output.Resolution != "" {
		return output.Resolution
	}
	return defaultVeoResolution
}

// ResolveVeoRequestOutput resolves public output fields to Veo's native
// parameters. It permits only explicitly registered legacy size aliases and
// validates the resulting values against the mapped model when it is known.
func ResolveVeoRequestOutput(req *relaycommon.TaskSubmitReq, modelName string) (VeoOutput, error) {
	if req == nil {
		return VeoOutput{}, fmt.Errorf("video request is required")
	}

	output := VeoOutput{}
	if value := strings.TrimSpace(req.Resolution); value != "" {
		resolution, err := relaycommon.NormalizeVideoOutputResolution(value)
		if err != nil {
			return VeoOutput{}, fmt.Errorf("resolution %q is not supported by Veo", value)
		}
		output.Resolution = resolution
	} else if value, ok, err := veoMetadataString(req.Metadata, "resolution"); err != nil {
		return VeoOutput{}, err
	} else if ok && strings.TrimSpace(value) != "" {
		resolution, err := relaycommon.NormalizeVideoOutputResolution(value)
		if err != nil {
			return VeoOutput{}, fmt.Errorf("metadata.resolution %q is not supported by Veo", value)
		}
		output.Resolution = resolution
	}

	if value := strings.TrimSpace(req.AspectRatio); value != "" {
		aspectRatio, err := relaycommon.NormalizeVideoAspectRatio(value)
		if err != nil {
			return VeoOutput{}, fmt.Errorf("aspect_ratio %q is not supported by Veo", value)
		}
		output.AspectRatio = aspectRatio
	} else if value, ok, err := veoMetadataString(req.Metadata, "aspect_ratio"); err != nil {
		return VeoOutput{}, err
	} else if ok && strings.TrimSpace(value) != "" {
		aspectRatio, err := relaycommon.NormalizeVideoAspectRatio(value)
		if err != nil {
			return VeoOutput{}, fmt.Errorf("metadata.aspect_ratio %q is not supported by Veo", value)
		}
		output.AspectRatio = aspectRatio
	}

	if size := strings.TrimSpace(req.Size); size != "" {
		legacyOutput, err := resolveVeoLegacySizeOutput(size)
		if err != nil {
			// Top-level resolution is authoritative. An old, otherwise unsupported
			// size value must not override it or be reinterpreted as a quality tier.
			if output.Resolution == "" {
				return VeoOutput{}, err
			}
		} else {
			if output.Resolution == "" && legacyOutput.Resolution != "" {
				output.Resolution = legacyOutput.Resolution
			}
			if output.AspectRatio == "" && legacyOutput.AspectRatio != "" {
				output.AspectRatio = legacyOutput.AspectRatio
			}
		}
	}

	if err := validateVeoOutput(output, modelName); err != nil {
		return VeoOutput{}, err
	}
	return output, nil
}

func veoMetadataString(metadata map[string]any, key string) (string, bool, error) {
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

func resolveVeoLegacySizeOutput(size string) (veoLegacySizeOutput, error) {
	if resolution, err := relaycommon.NormalizeVideoOutputResolution(size); err == nil {
		return veoLegacySizeOutput{Resolution: resolution}, nil
	}

	canonical, _, _, pixelSize, err := relaycommon.NormalizeVideoPixelSize(strings.NewReplacer("*", "x").Replace(size))
	if err != nil {
		return veoLegacySizeOutput{}, fmt.Errorf("size %q is not a supported Veo legacy size: %w", size, err)
	}
	if !pixelSize {
		return veoLegacySizeOutput{}, fmt.Errorf("size %q is not supported by Veo; use aspect_ratio and resolution instead", size)
	}
	output, ok := veoLegacySizeOutputs[canonical]
	if !ok {
		return veoLegacySizeOutput{}, fmt.Errorf("size %q is not supported by Veo; use aspect_ratio and resolution instead", size)
	}
	return output, nil
}

func validateVeoOutput(output VeoOutput, modelName string) error {
	if output.AspectRatio != "" && output.AspectRatio != "16:9" && output.AspectRatio != "9:16" {
		return fmt.Errorf("aspect_ratio %q is not supported by Veo; use 16:9 or 9:16", output.AspectRatio)
	}
	if output.Resolution == "" {
		return nil
	}
	switch output.Resolution {
	case "720p", "1080p":
		return nil
	case "4k":
		if modelName == "" || strings.Contains(strings.ToLower(modelName), "veo-3.1") {
			return nil
		}
		return fmt.Errorf("resolution %q is only supported by Veo 3.1 models", output.Resolution)
	default:
		return fmt.Errorf("resolution %q is not supported by Veo; use 720p, 1080p, or 4k", output.Resolution)
	}
}

// SizeToVeoResolution returns an explicit legacy size mapping. It returns an
// empty string for unknown values instead of inferring a quality tier.
func SizeToVeoResolution(size string) string {
	output, err := resolveVeoLegacySizeOutput(size)
	if err != nil {
		return ""
	}
	return output.Resolution
}

// SizeToVeoAspectRatio returns an explicit legacy size mapping. It returns an
// empty string for a quality-label-only or unknown value.
func SizeToVeoAspectRatio(size string) string {
	output, err := resolveVeoLegacySizeOutput(size)
	if err != nil {
		return ""
	}
	return output.AspectRatio
}

// VeoResolutionRatio returns the pricing multiplier for the given resolution.
// Standard resolutions (720p, 1080p) return 1.0.
// 4K returns a model-specific multiplier based on Google's official pricing.
func VeoResolutionRatio(modelName, resolution string) float64 {
	if resolution != "4k" {
		return 1.0
	}
	// 4K multipliers derived from Vertex AI official pricing (video+audio base):
	//   veo-3.1-generate:      $0.60 / $0.40 = 1.5
	//   veo-3.1-fast-generate: $0.35 / $0.15 ≈ 2.333
	// Veo 3.0 models do not support 4K; return 1.0 as fallback.
	if strings.Contains(modelName, "3.1-fast-generate") {
		return 2.333333
	}
	if strings.Contains(modelName, "3.1-generate") || strings.Contains(modelName, "3.1") {
		return 1.5
	}
	return 1.0
}
