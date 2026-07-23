package service

import (
	"errors"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const ginKeyVideoRequestFeatures = "video_request_features"

type videoRequestFeaturesResult struct {
	Features VideoRequestFeatures
	Err      error
}

func isVideoGenerationSubmit(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil {
		return false
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	return path == "/v1/videos" || path == "/v1/videos/generations" || path == "/v1/video/generations"
}

func GetVideoRequestFeatures(c *gin.Context) (VideoRequestFeatures, error) {
	if !isVideoGenerationSubmit(c) {
		return VideoRequestFeatures{}, nil
	}
	if cached, ok := c.Get(ginKeyVideoRequestFeatures); ok {
		if result, valid := cached.(videoRequestFeaturesResult); valid {
			return result.Features, result.Err
		}
	}

	features, err := extractVideoRequestFeatures(c)
	if err != nil {
		err = &VideoRequestFeaturesError{Err: err}
	}
	c.Set(ginKeyVideoRequestFeatures, videoRequestFeaturesResult{Features: features, Err: err})
	return features, err
}

type VideoRequestFeaturesError struct {
	Err error
}

func (e *VideoRequestFeaturesError) Error() string {
	return e.Err.Error()
}

func (e *VideoRequestFeaturesError) Unwrap() error {
	return e.Err
}

// videoRequestOutputError marks a malformed public video output request. It
// lets the distributor retain the API-level invalid_video_output error code
// when validation happens during channel selection.
type videoRequestOutputError struct {
	err error
}

func (e *videoRequestOutputError) Error() string {
	return e.err.Error()
}

func (e *videoRequestOutputError) Unwrap() error {
	return e.err
}

// IsVideoRequestOutputError reports whether routing rejected the request's
// normalized public video output fields.
func IsVideoRequestOutputError(err error) bool {
	var outputErr *videoRequestOutputError
	return errors.As(err, &outputErr)
}

// videoRequestFeatureDTOError means the JSON itself was valid, but it could
// not be represented by the common task DTO used only for routing features.
// An unconstrained channel may still understand that upstream-specific shape.
type videoRequestFeatureDTOError struct {
	err error
}

func (e *videoRequestFeatureDTOError) Error() string {
	return e.err.Error()
}

func (e *videoRequestFeatureDTOError) Unwrap() error {
	return e.err
}

func isVideoRequestFeatureDTOError(err error) bool {
	var dtoErr *videoRequestFeatureDTOError
	return errors.As(err, &dtoErr)
}

func extractVideoRequestFeatures(c *gin.Context) (VideoRequestFeatures, error) {
	features := VideoRequestFeatures{ContentType: c.GetHeader("Content-Type")}
	contentType := strings.ToLower(features.ContentType)
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return features, err
		}
		body, err := storage.Bytes()
		if err != nil {
			return features, err
		}
		return extractJSONVideoRequestFeatures(body, features)
	case strings.HasPrefix(contentType, "multipart/form-data"):
		return extractMultipartVideoRequestFeatures(c, features)
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return features, err
		}
		body, err := storage.Bytes()
		if err != nil {
			return features, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return features, err
		}
		return extractFormVideoRequestFeatures(values, nil, features)
	default:
		return features, nil
	}
}

func extractJSONVideoRequestFeatures(body []byte, features VideoRequestFeatures) (VideoRequestFeatures, error) {
	if !gjson.ValidBytes(body) {
		return features, fmt.Errorf("invalid JSON request body")
	}
	var request relaycommon.TaskSubmitReq
	if err := common.Unmarshal(body, &request); err != nil {
		return features, &videoRequestFeatureDTOError{err: err}
	}
	output, err := relaycommon.NormalizeTaskSubmitVideoOutput(&request)
	if err != nil {
		return features, &videoRequestOutputError{err: err}
	}

	features.Images = countNonEmptyFeatureStrings(request.Images) +
		countNonEmptyFeatureStrings(request.ImageURLs) +
		countNonEmptyFeatureStrings(request.InputStartFrames) +
		countNonEmptyFeatureStrings(request.InputImageReferences) +
		countNonEmptyFeatureStrings(request.MetadataStartFrames) +
		countNonEmptyFeatureStrings([]string{request.Image, request.InputReference})
	features.Videos = countNonEmptyFeatureStrings(request.Videos) + countNonEmptyFeatureStrings(request.VideoURLs)
	features.Audios = countNonEmptyFeatureStrings(request.Audios) + countNonEmptyFeatureStrings(request.AudioURLs)
	if len(request.Content) > 0 {
		genericContent, profiledContent := countTaskContentMedia(request.Content)
		features.Images += genericContent.Images
		features.Videos += genericContent.Videos
		features.Audios += genericContent.Audios
		features.profiledContent = &profiledContent
	}
	duration := parseJSONDuration(body)
	features.Duration = duration.Value
	features.durationUnnormalized = duration.Unnormalized
	features.AspectRatio = output.AspectRatio
	if output.HasPixelSize() {
		features.Size = output.Size
	}
	features.Resolution = output.EffectiveResolution()
	features.providerResolutionHints = parseJSONVideoProviderResolutionHints(body)
	return features, nil
}

func countTaskContentMedia(content []relaycommon.TaskContentItem) (videoMediaCounts, videoMediaCounts) {
	generic := videoMediaCounts{}
	profiled := videoMediaCounts{}
	for _, item := range content {
		if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
			generic.Images++
		}
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			generic.Videos++
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			generic.Audios++
		}
		switch item.Type {
		case "image_url":
			if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
				profiled.Images++
			} else {
				profiled.Invalid = true
			}
		case "video_url":
			if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
				profiled.Videos++
			} else {
				profiled.Invalid = true
			}
		case "audio_url":
			if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
				profiled.Audios++
			} else {
				profiled.Invalid = true
			}
		case "text":
			if strings.TrimSpace(item.Text) != "" {
				profiled.Text++
			} else {
				profiled.Invalid = true
			}
		default:
			profiled.Invalid = true
		}
	}
	return generic, profiled
}

func extractMultipartVideoRequestFeatures(c *gin.Context, features VideoRequestFeatures) (VideoRequestFeatures, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return features, err
	}
	defer form.RemoveAll()
	return extractFormVideoRequestFeatures(form.Value, form.File, features)
}

func extractFormVideoRequestFeatures(values url.Values, files map[string][]*multipart.FileHeader, features VideoRequestFeatures) (VideoRequestFeatures, error) {
	features.Images = countFormMedia(values, files, "image", "images", "image_urls", "input_reference")
	features.Videos = countFormMedia(values, files, "video", "videos", "video_url", "video_urls")
	features.Audios = countFormMedia(values, files, "audio", "audios", "audio_url", "audio_urls")
	// Jimeng Dimensio consumes its binary references from numbered file fields.
	// Count them here so an exact media capability rule is enforced before the
	// channel is selected.
	features.Images += countNumberedFormFiles(files, "image_file_", 9)
	features.Videos += countNumberedFormFiles(files, "video_file_", 3)
	features.Audios += countNumberedFormFiles(files, "audio_file_", 3)
	duration := parseFormDuration(values)
	features.Duration = duration.Value
	features.durationUnnormalized = duration.Unnormalized
	request, err := taskSubmitReqFromFormValues(values)
	if err != nil {
		return features, &videoRequestFeatureDTOError{err: err}
	}
	output, err := relaycommon.NormalizeTaskSubmitVideoOutput(&request)
	if err != nil {
		return features, &videoRequestOutputError{err: err}
	}
	features.AspectRatio = output.AspectRatio
	if output.HasPixelSize() {
		features.Size = output.Size
	}
	features.Resolution = output.EffectiveResolution()
	return features, nil
}

// taskSubmitReqFromFormValues mirrors common.UnmarshalBodyReusable's form
// conversion so routing evaluates metadata output fallbacks exactly as the
// selected task adaptor will.
func taskSubmitReqFromFormValues(values url.Values) (relaycommon.TaskSubmitReq, error) {
	formMap := make(map[string]any, len(values))
	for key, entries := range values {
		switch len(entries) {
		case 0:
			continue
		case 1:
			formMap[key] = entries[0]
		default:
			formMap[key] = entries
		}
	}

	body, err := common.Marshal(formMap)
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	var request relaycommon.TaskSubmitReq
	if err := common.Unmarshal(body, &request); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	return request, nil
}

func countFormMedia(values url.Values, files map[string][]*multipart.FileHeader, fields ...string) int {
	count := 0
	for _, field := range fields {
		for _, value := range values[field] {
			if strings.TrimSpace(value) != "" {
				count++
			}
		}
		count += len(files[field])
	}
	return count
}

func countNumberedFormFiles(files map[string][]*multipart.FileHeader, prefix string, max int) int {
	count := 0
	for index := 1; index <= max; index++ {
		count += len(files[prefix+strconv.Itoa(index)])
	}
	return count
}

func countNonEmptyFeatureStrings(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

type videoRequestDurationParseResult struct {
	Value        *int
	Unnormalized bool
}

func parseJSONDuration(body []byte) videoRequestDurationParseResult {
	fields := make(map[string]interface{})
	if err := common.Unmarshal(body, &fields); err != nil {
		return videoRequestDurationParseResult{Unnormalized: true}
	}
	return resolveVideoRequestDuration(fields)
}

func parseFormDuration(values url.Values) videoRequestDurationParseResult {
	fields := make(map[string]interface{}, 3)
	for _, field := range []string{"duration", "seconds", "metadata"} {
		entries, exists := values[field]
		if !exists {
			continue
		}
		switch len(entries) {
		case 0:
			fields[field] = ""
		case 1:
			fields[field] = entries[0]
		default:
			fields[field] = append([]string(nil), entries...)
		}
	}
	return resolveVideoRequestDuration(fields)
}

func resolveVideoRequestDuration(fields map[string]interface{}) videoRequestDurationParseResult {
	duration, durationProvided, durationValid := videoRequestDurationField(fields, "duration")
	seconds, secondsProvided, secondsValid := videoRequestDurationField(fields, "seconds")
	metadataDuration, metadataProvided, metadataValid := videoRequestMetadataDuration(fields)

	result := videoRequestDurationParseResult{
		Unnormalized: (durationProvided && !durationValid) ||
			(secondsProvided && !secondsValid) ||
			(metadataProvided && !metadataValid),
	}
	canonicalDuration := 0
	canonicalField := ""
	if durationValid {
		canonicalDuration = duration
		canonicalField = "duration"
	} else if secondsValid {
		canonicalDuration = seconds
		canonicalField = "seconds"
	} else if metadataValid {
		canonicalDuration = metadataDuration
		canonicalField = "metadata.duration"
	}
	if canonicalField == "" {
		return result
	}
	for _, alias := range []struct {
		field string
		value int
		valid bool
	}{
		{field: "duration", value: duration, valid: durationValid},
		{field: "seconds", value: seconds, valid: secondsValid},
		{field: "metadata.duration", value: metadataDuration, valid: metadataValid},
	} {
		if alias.valid && alias.value != canonicalDuration {
			result.Unnormalized = true
			break
		}
	}
	result.Value = common.GetPointer(canonicalDuration)
	return result
}

func videoRequestMetadataDuration(fields map[string]interface{}) (int, bool, bool) {
	value, exists := fields["metadata"]
	if !exists || value == nil {
		return 0, false, false
	}

	var metadata map[string]interface{}
	switch typed := value.(type) {
	case map[string]interface{}:
		metadata = typed
	case string:
		if err := common.UnmarshalJsonStr(typed, &metadata); err != nil {
			return 0, false, false
		}
	default:
		return 0, false, false
	}
	return videoRequestDurationField(metadata, "duration")
}

func videoRequestDurationField(fields map[string]interface{}, field string) (int, bool, bool) {
	value, exists := fields[field]
	if !exists || value == nil {
		return 0, false, false
	}
	duration, ok := parseVideoRequestDurationValue(value)
	return duration, true, ok
}

func parseJSONVideoProviderResolutionHints(body []byte) videoProviderResolutionHints {
	return videoProviderResolutionHints{
		soraParameters: parseJSONVideoResolutionHint(body, "parameters.resolution"),
		aggcParams:     parseJSONVideoResolutionHint(body, "params.resolution"),
	}
}

func parseJSONVideoResolutionHint(body []byte, field string) string {
	result := gjson.GetBytes(body, field)
	if !result.Exists() || result.Type == gjson.Null {
		return ""
	}
	return parseVideoResolution(result.String())
}

func parseVideoResolution(value string) string {
	resolution, err := relaycommon.NormalizeVideoOutputResolution(value)
	if err != nil {
		return ""
	}
	return resolution
}

func parseVideoRequestDurationValue(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case string:
		return parseDurationString(typed)
	case int:
		return typed, typed > 0
	case int64:
		if typed > 0 && typed <= math.MaxInt32 {
			return int(typed), true
		}
	case float64:
		if typed > 0 && typed <= math.MaxInt32 && typed == math.Trunc(typed) {
			return int(typed), true
		}
	case float32:
		numeric := float64(typed)
		if numeric > 0 && numeric <= math.MaxInt32 && numeric == math.Trunc(numeric) {
			return int(numeric), true
		}
	}
	return 0, false
}

func parseDurationString(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			break
		}
	}
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}
