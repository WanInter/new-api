package service

import (
	"errors"
	"fmt"
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
		return extractFormVideoRequestFeatures(values, nil, features), nil
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
	features.Duration = parseJSONDuration(body)
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
	return extractFormVideoRequestFeatures(form.Value, form.File, features), nil
}

func extractFormVideoRequestFeatures(values url.Values, files map[string][]*multipart.FileHeader, features VideoRequestFeatures) VideoRequestFeatures {
	features.Images = countFormMedia(values, files, "image", "images", "image_urls", "input_reference")
	features.Videos = countFormMedia(values, files, "video", "videos", "video_url", "video_urls")
	features.Audios = countFormMedia(values, files, "audio", "audios", "audio_url", "audio_urls")
	for _, field := range []string{"duration", "seconds"} {
		if parsed, ok := parseDurationString(values.Get(field)); ok {
			features.Duration = common.GetPointer(parsed)
			break
		}
	}
	return features
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

func countNonEmptyFeatureStrings(values []string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func parseJSONDuration(body []byte) *int {
	for _, field := range []string{"duration", "seconds"} {
		result := gjson.GetBytes(body, field)
		if !result.Exists() || result.Type == gjson.Null {
			continue
		}
		if parsed, ok := parseDurationString(result.String()); ok {
			return common.GetPointer(parsed)
		}
	}
	return nil
}

func parseDurationString(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
		value = strings.TrimSuffix(value, suffix)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}
