package gemini

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const maxVeoImageSize = 20 * 1024 * 1024 // 20 MB

const ginKeyVeoResolvedImageInput = "gemini_veo_resolved_image_input"

type veoResolvedImageInput struct {
	image *VeoImageInput
}

// ResolveVeoImageInput validates and converts the one image input supported
// by Veo. The public task request accepts multiple media aliases, but Veo can
// only send one image and cannot dereference remote HTTP(S) URLs. Rejecting
// unsupported values here prevents an image-to-video request from silently
// becoming text-to-video after model mapping and before billing.
func ResolveVeoImageInput(c *gin.Context, req *relaycommon.TaskSubmitReq) (*VeoImageInput, error) {
	if req == nil {
		return nil, fmt.Errorf("video request is required")
	}
	if c != nil {
		if cached, ok := c.Get(ginKeyVeoResolvedImageInput); ok {
			if resolved, ok := cached.(veoResolvedImageInput); ok {
				return resolved.image, nil
			}
		}
	}

	if err := validateVeoUnsupportedMedia(req); err != nil {
		return nil, err
	}

	multipartImage, hasMultipartImage, err := extractVeoMultipartImage(c)
	if err != nil {
		return nil, err
	}
	references := collectVeoImageReferences(req)

	var image *VeoImageInput
	switch {
	case hasMultipartImage && len(references) > 0:
		return nil, fmt.Errorf("Veo accepts one image input; do not combine multipart input_reference with JSON image fields")
	case hasMultipartImage:
		image = multipartImage
	case len(references) > 1:
		return nil, fmt.Errorf("Veo supports at most one image input")
	case len(references) == 1:
		image, err = parseVeoImageReference(references[0])
		if err != nil {
			return nil, err
		}
	}

	if c != nil {
		c.Set(ginKeyVeoResolvedImageInput, veoResolvedImageInput{image: image})
	}
	return image, nil
}

func validateVeoUnsupportedMedia(req *relaycommon.TaskSubmitReq) error {
	if hasNonEmptyVeoMedia(req.Videos, req.VideoURLs) {
		return fmt.Errorf("Veo does not support video input")
	}
	if hasNonEmptyVeoMedia(req.Audios, req.AudioURLs) {
		return fmt.Errorf("Veo does not support audio input")
	}
	if hasNonEmptyVeoMedia(req.InputStartFrames, req.InputImageReferences, req.MetadataStartFrames) {
		return fmt.Errorf("Veo does not support start-frame or image-reference inputs; use one images value instead")
	}
	for _, item := range req.Content {
		if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
			return fmt.Errorf("Veo does not support content image inputs; use one images value instead")
		}
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			return fmt.Errorf("Veo does not support content video inputs")
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			return fmt.Errorf("Veo does not support content audio inputs")
		}
	}
	return nil
}

func hasNonEmptyVeoMedia(collections ...[]string) bool {
	for _, values := range collections {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

func collectVeoImageReferences(req *relaycommon.TaskSubmitReq) []string {
	references := make([]string, 0, len(req.Images)+len(req.ImageURLs)+2)
	appendValues := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			references = append(references, value)
		}
	}
	appendValues(req.Images...)
	// ValidateBasicTaskRequest promotes the legacy image field into images.
	// Only consult Image as a fallback for direct adaptor callers that bypass
	// that common validation path; otherwise one user-supplied image would be
	// counted twice.
	if !hasNonEmptyVeoMedia(req.Images) {
		appendValues(req.Image)
	}
	appendValues(req.ImageURLs...)
	appendValues(req.InputReference)
	return references
}

func parseVeoImageReference(value string) (*VeoImageInput, error) {
	if isHTTPImageURL(value) {
		return nil, fmt.Errorf("Veo image input does not support HTTP(S) URLs; use a data URI, raw base64, or multipart input_reference")
	}
	image := ParseImageInput(value)
	if image == nil {
		return nil, fmt.Errorf("Veo image input must be a valid data URI, raw base64, or multipart input_reference")
	}
	return image, nil
}

func isHTTPImageURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// ExtractMultipartImage reads the first `input_reference` file from a multipart
// form upload and returns a VeoImageInput. Returns nil if no file is present.
func ExtractMultipartImage(c *gin.Context, info *relaycommon.RelayInfo) *VeoImageInput {
	image, _, err := extractVeoMultipartImage(c)
	if err != nil {
		return nil
	}
	if image != nil && info != nil && info.TaskRelayInfo != nil {
		info.Action = constant.TaskActionGenerate
	}
	return image
}

func extractVeoMultipartImage(c *gin.Context) (*VeoImageInput, bool, error) {
	if c == nil || c.Request == nil || !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return nil, false, nil
	}
	mf, err := c.MultipartForm()
	if err != nil {
		return nil, false, fmt.Errorf("read multipart Veo image input: %w", err)
	}
	for field, files := range mf.File {
		if field != "input_reference" && len(files) > 0 {
			return nil, false, fmt.Errorf("Veo only supports multipart image uploads in input_reference; file field %q is not supported", field)
		}
	}
	files := mf.File["input_reference"]
	if len(files) == 0 {
		return nil, false, nil
	}
	if len(files) != 1 {
		return nil, false, fmt.Errorf("Veo supports at most one multipart input_reference image")
	}

	fh := files[0]
	if fh.Size > maxVeoImageSize {
		return nil, false, fmt.Errorf("Veo input_reference image exceeds the %d MB limit", maxVeoImageSize/(1024*1024))
	}
	file, err := fh.Open()
	if err != nil {
		return nil, false, fmt.Errorf("open Veo input_reference image: %w", err)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, false, fmt.Errorf("read Veo input_reference image: %w", err)
	}
	if len(fileBytes) == 0 {
		return nil, false, fmt.Errorf("Veo input_reference image cannot be empty")
	}

	mimeType := strings.TrimSpace(fh.Header.Get("Content-Type"))
	if mimeType == "" || strings.EqualFold(mimeType, "application/octet-stream") {
		mimeType = http.DetectContentType(fileBytes)
	}
	mimeType = strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, false, fmt.Errorf("Veo input_reference must be an image file")
	}

	return &VeoImageInput{
		BytesBase64Encoded: base64.StdEncoding.EncodeToString(fileBytes),
		MimeType:           mimeType,
	}, true, nil
}

// ParseImageInput parses an image string (data URI or raw base64) into a
// VeoImageInput. Returns nil if the input is empty or invalid.
func ParseImageInput(imageStr string) *VeoImageInput {
	imageStr = strings.TrimSpace(imageStr)
	if imageStr == "" {
		return nil
	}

	if strings.HasPrefix(imageStr, "data:") {
		return parseDataURI(imageStr)
	}

	raw, err := base64.StdEncoding.DecodeString(imageStr)
	if err != nil {
		return nil
	}
	return &VeoImageInput{
		BytesBase64Encoded: imageStr,
		MimeType:           http.DetectContentType(raw),
	}
}

func parseDataURI(uri string) *VeoImageInput {
	// data:image/png;base64,iVBOR...
	rest := uri[len("data:"):]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return nil
	}
	meta := rest[:idx]
	b64 := rest[idx+1:]
	if b64 == "" {
		return nil
	}

	parts := strings.Split(meta, ";")
	if len(parts) == 0 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(parts[0])), "image/") {
		return nil
	}
	isBase64 := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			isBase64 = true
			break
		}
	}
	if !isBase64 {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(decoded) == 0 {
		return nil
	}

	return &VeoImageInput{
		BytesBase64Encoded: base64.StdEncoding.EncodeToString(decoded),
		MimeType:           strings.TrimSpace(parts[0]),
	}
}
