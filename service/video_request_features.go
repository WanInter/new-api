package service

import (
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
	return path == "/v1/videos" || path == "/v1/video/generations"
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
	c.Set(ginKeyVideoRequestFeatures, videoRequestFeaturesResult{Features: features, Err: err})
	return features, err
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
		return features, err
	}

	images := make(map[string]struct{})
	videos := make(map[string]struct{})
	audios := make(map[string]struct{})
	addStrings(images, request.Images...)
	addStrings(images, request.Image, request.InputReference)
	addStrings(videos, request.Videos...)
	addStrings(audios, request.Audios...)

	for _, item := range request.Content {
		if item.ImageURL != nil {
			addStrings(images, item.ImageURL.URL)
		}
		if item.VideoURL != nil {
			addStrings(videos, item.VideoURL.URL)
		}
		if item.AudioURL != nil {
			addStrings(audios, item.AudioURL.URL)
		}
	}

	collectJSONMediaStrings(body, images, "image", "images", "image_urls", "input_reference", "input.start_frames", "metadata.start_frames")
	collectJSONMediaStrings(body, videos, "video", "videos", "video_url", "video_urls")
	collectJSONMediaStrings(body, audios, "audio", "audios", "audio_url", "audio_urls")
	collectJSONReferenceObjects(body, images, "input.image_references")
	collectJSONContent(body, images, videos, audios)

	features.Images = len(images)
	features.Videos = len(videos)
	features.Audios = len(audios)
	features.Duration = parseJSONDuration(body)
	return features, nil
}

func collectJSONMediaStrings(body []byte, target map[string]struct{}, paths ...string) {
	for _, path := range paths {
		result := gjson.GetBytes(body, path)
		if result.IsArray() {
			for _, item := range result.Array() {
				addStrings(target, item.String())
			}
			continue
		}
		addStrings(target, result.String())
	}
}

func collectJSONReferenceObjects(body []byte, target map[string]struct{}, path string) {
	for _, item := range gjson.GetBytes(body, path).Array() {
		value := item.String()
		if item.IsObject() {
			value = item.Get("url").String()
		}
		addStrings(target, value)
	}
}

func collectJSONContent(body []byte, images, videos, audios map[string]struct{}) {
	for _, item := range gjson.GetBytes(body, "content").Array() {
		switch item.Get("type").String() {
		case "image_url":
			addStrings(images, firstNonEmptyFeatureString(item.Get("image_url.url").String(), item.Get("image_url").String()))
		case "video_url":
			addStrings(videos, firstNonEmptyFeatureString(item.Get("video_url.url").String(), item.Get("video_url").String()))
		case "audio_url":
			addStrings(audios, firstNonEmptyFeatureString(item.Get("audio_url.url").String(), item.Get("audio_url").String()))
		}
	}
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

func addStrings(target map[string]struct{}, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			target[value] = struct{}{}
		}
	}
}

func firstNonEmptyFeatureString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
