package sora

import (
	"mime/multipart"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func applyTokenStackJSONRequest(body map[string]interface{}) {
	if body == nil {
		return
	}
	applyTokenStackMediaFields(body)
	mapDurationToSoraSeconds(body)
	if strings.TrimSpace(stringValue(body["size"])) == "" {
		if size := tokenStackSizeFromLegacyFields(body); size != "" {
			body["size"] = size
		}
	}

	// Fields without a documented TokenStack mapping stay in the request. The
	// upstream can reject them instead of this adaptor silently discarding data.
}

func tokenStackSizeFromLegacyFields(body map[string]interface{}) string {
	ratio := firstNonEmpty(stringValue(body["aspect_ratio"]), stringValue(body["ratio"]))
	resolution := strings.ToLower(strings.TrimSpace(stringValue(body["resolution"])))
	if resolution != "" && resolution != "720p" {
		return ""
	}

	switch ratio {
	case "16:9":
		return "1280x720"
	case "9:16":
		return "720x1280"
	default:
		return ""
	}
}

func applyTokenStackMediaFields(body map[string]interface{}) {
	data, err := common.Marshal(body)
	if err != nil {
		return
	}
	var req relaycommon.TaskSubmitReq
	if err := common.Unmarshal(data, &req); err != nil {
		return
	}

	images := appendNonEmptyTokenStackURLs(nil, req.Images...)
	images = appendNonEmptyTokenStackURLs(images, req.Image)
	images = appendNonEmptyTokenStackURLs(images, req.ImageURLs...)
	images = appendNonEmptyTokenStackURLs(images, req.InputReference)
	images = appendNonEmptyTokenStackURLs(images, req.InputStartFrames...)
	images = appendNonEmptyTokenStackURLs(images, req.InputImageReferences...)
	images = appendNonEmptyTokenStackURLs(images, req.MetadataStartFrames...)

	videos := appendNonEmptyTokenStackURLs(nil, req.Videos...)
	videos = appendNonEmptyTokenStackURLs(videos, req.VideoURLs...)
	audios := appendNonEmptyTokenStackURLs(nil, req.Audios...)
	audios = appendNonEmptyTokenStackURLs(audios, req.AudioURLs...)

	for _, item := range req.Content {
		if item.ImageURL != nil {
			images = appendNonEmptyTokenStackURLs(images, item.ImageURL.URL)
		}
		if item.VideoURL != nil {
			videos = appendNonEmptyTokenStackURLs(videos, item.VideoURL.URL)
		}
		if item.AudioURL != nil {
			audios = appendNonEmptyTokenStackURLs(audios, item.AudioURL.URL)
		}
	}

	if len(images) > 0 {
		body["images"] = images
	}
	if len(videos) > 0 {
		body["videos"] = videos
	}
	if len(audios) > 0 {
		body["audios"] = audios
	}
}

func appendNonEmptyTokenStackURLs(target []string, values ...string) []string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			target = append(target, value)
		}
	}
	return target
}

func applySoraModelJSONProfile(body map[string]interface{}, profile soraModelProfile) {
	if profile.DropSecondsField {
		moveSecondsFieldToDuration(body)
	}

	switch profile.JSONTransform {
	case requestTransformOpenAIContent:
		applyOpenAIContentRequest(body)
	case requestTransformOtoySeedanceReference:
		applyOtoySeedanceMiniReferenceRequest(body)
	case requestTransformVeoReferenceImages:
		applyVeoReferenceImages(body)
	}
}

func applySoraModelJSONFinalProfile(body map[string]interface{}, profile soraModelProfile) {
	if profile.JSONFinalTransform == requestTransformTokenStackSora15s {
		applyTokenStackJSONRequest(body)
	}
}

func applySoraModelMultipartProfile(writer *multipart.Writer, values map[string][]string, profile soraModelProfile) {
	if profile.MultipartTransform == requestTransformOtoySeedanceReference {
		writeOtoySeedanceMiniReferenceMultipartFields(writer, values)
	}
}

func moveSecondsFieldToDuration(body map[string]interface{}) {
	if body == nil {
		return
	}
	if _, exists := body["duration"]; !exists {
		if seconds, exists := body["seconds"]; exists {
			body["duration"] = seconds
		}
	}
	delete(body, "seconds")
}

func applyOpenAIContentRequest(body map[string]interface{}) {
	if body == nil {
		return
	}

	hadContent := hasContentItems(body["content"])
	content, _ := body["content"].([]interface{})
	legacyContent := make([]interface{}, 0)
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", body["images"])
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", body["image"])
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", body["image_urls"])
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", body["input_reference"])
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", nestedRequestValue(body, "input", "start_frames"))
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", nestedRequestValue(body, "input", "image_references"))
	legacyContent = appendLegacyURLContent(legacyContent, "image_url", nestedRequestValue(body, "metadata", "start_frames"))
	legacyContent = appendLegacyURLContent(legacyContent, "video_url", body["video"])
	legacyContent = appendLegacyURLContent(legacyContent, "video_url", body["videos"])
	legacyContent = appendLegacyURLContent(legacyContent, "video_url", body["video_url"])
	legacyContent = appendLegacyURLContent(legacyContent, "video_url", body["video_urls"])
	legacyContent = appendLegacyURLContent(legacyContent, "audio_url", body["audio"])
	legacyContent = appendLegacyURLContent(legacyContent, "audio_url", body["audios"])
	legacyContent = appendLegacyURLContent(legacyContent, "audio_url", body["audio_url"])
	legacyContent = appendLegacyURLContent(legacyContent, "audio_url", body["audio_urls"])
	content = append(legacyContent, content...)
	if !hadContent {
		if prompt, ok := body["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": prompt,
			})
		}
	}
	body["content"] = content

	for _, field := range []string{
		"images", "image", "image_urls", "input_reference",
		"video", "videos", "video_url", "video_urls",
		"audio", "audios", "audio_url", "audio_urls",
	} {
		delete(body, field)
	}
	deleteNestedRequestFields(body, "input", "start_frames", "image_references")
	deleteNestedRequestFields(body, "metadata", "start_frames")
}

func nestedRequestValue(body map[string]interface{}, parent, field string) interface{} {
	value, ok := nestedRequestMap(body, parent)
	if !ok {
		return nil
	}
	return value[field]
}

func deleteNestedRequestFields(body map[string]interface{}, parent string, fields ...string) {
	value, ok := nestedRequestMap(body, parent)
	if !ok {
		return
	}
	for _, field := range fields {
		delete(value, field)
	}
	if len(value) == 0 {
		delete(body, parent)
	}
}

func nestedRequestMap(body map[string]interface{}, parent string) (map[string]interface{}, bool) {
	switch value := body[parent].(type) {
	case map[string]interface{}:
		return value, true
	case string:
		parsed := make(map[string]interface{})
		if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
			return nil, false
		}
		body[parent] = parsed
		return parsed, true
	default:
		return nil, false
	}
}

func hasContentItems(value interface{}) bool {
	items, ok := value.([]interface{})
	return ok && len(items) > 0
}

func appendLegacyURLContent(content []interface{}, contentType string, value interface{}) []interface{} {
	appendURL := func(rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		content = append(content, map[string]interface{}{
			"type": contentType,
			contentType: map[string]interface{}{
				"url": rawURL,
			},
		})
	}

	switch values := value.(type) {
	case string:
		appendURL(values)
	case map[string]interface{}:
		if rawURL, ok := values["url"].(string); ok {
			appendURL(rawURL)
		}
	case []string:
		for _, rawURL := range values {
			appendURL(rawURL)
		}
	case []interface{}:
		for _, item := range values {
			switch item := item.(type) {
			case string:
				appendURL(item)
			case map[string]interface{}:
				rawURL, _ := item["url"].(string)
				appendURL(rawURL)
			}
		}
	}
	return content
}

func writeOtoySeedanceMiniReferenceMultipartFields(writer *multipart.Writer, values map[string][]string) {
	writeValues := func(key string, vals []string) {
		for _, value := range vals {
			value = strings.TrimSpace(value)
			if value != "" {
				_ = writer.WriteField(key, value)
			}
		}
	}

	mappedAspectRatio, mappedResolution, mappedSize := otoyAspectRatioAndResolutionFromForm(values)
	transformedFields := map[string]bool{
		"model":           true,
		"duration":        true,
		"seconds":         true,
		"image_urls":      true,
		"images":          true,
		"image":           true,
		"input_reference": true,
		"video_urls":      true,
		"videos":          true,
		"video":           true,
		"video_url":       true,
		"audio_urls":      true,
		"audios":          true,
		"audio":           true,
		"audio_url":       true,
	}
	if mappedSize {
		transformedFields["size"] = true
	}

	for key, fieldValues := range values {
		if !transformedFields[key] {
			writeValues(key, fieldValues)
		}
	}

	if mappedSize {
		_ = writer.WriteField("aspect_ratio", mappedAspectRatio)
		_ = writer.WriteField("resolution", mappedResolution)
	}
	writeOtoyMultipartDuration(writer, values)
	writeValues("image_urls", appendOtoyImageValues(nil, values))
	writeValues("video_urls", appendOtoyVideoValues(nil, values))
	writeValues("audio_urls", appendOtoyAudioValues(nil, values))
}

func writeOtoyMultipartDuration(writer *multipart.Writer, values map[string][]string) {
	durations, hasDuration := values["duration"]
	if !hasDuration {
		durations = values["seconds"]
	}
	for _, duration := range durations {
		_ = writer.WriteField("duration", duration)
	}
}

func appendOtoyImageValues(target []string, values map[string][]string) []string {
	for _, key := range []string{"image_urls", "images", "image", "input_reference"} {
		target = append(target, values[key]...)
	}
	return target
}

func appendOtoyVideoValues(target []string, values map[string][]string) []string {
	for _, key := range []string{"video_urls", "videos", "video", "video_url"} {
		target = append(target, values[key]...)
	}
	return target
}

func appendOtoyAudioValues(target []string, values map[string][]string) []string {
	for _, key := range []string{"audio_urls", "audios", "audio", "audio_url"} {
		target = append(target, values[key]...)
	}
	return target
}

func otoyAspectRatioAndResolutionFromForm(values map[string][]string) (string, string, bool) {
	if len(values["aspect_ratio"]) > 0 || len(values["resolution"]) > 0 {
		return "", "", false
	}
	return otoyAspectRatioAndResolutionFromSize(firstFormValue(values, "size"))
}

func firstFormValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func applyOtoySeedanceMiniReferenceRequest(body map[string]interface{}) {
	if body == nil {
		return
	}

	moveSecondsFieldToDuration(body)
	applyOtoySizeMapping(body)
	applyOtoyMediaAliases(body)
}

func applyOtoySizeMapping(body map[string]interface{}) {
	if strings.TrimSpace(stringValue(body["aspect_ratio"])) != "" || strings.TrimSpace(stringValue(body["resolution"])) != "" {
		return
	}
	aspectRatio, resolution, ok := otoyAspectRatioAndResolutionFromSize(stringValue(body["size"]))
	if !ok {
		return
	}
	body["aspect_ratio"] = aspectRatio
	body["resolution"] = resolution
	delete(body, "size")
}
func otoyAspectRatioAndResolutionFromSize(size string) (string, string, bool) {
	switch strings.TrimSpace(size) {
	case "1280x720":
		return "16:9", "720p", true
	case "720x1280":
		return "9:16", "720p", true
	default:
		return "", "", false
	}
}

func applyOtoyMediaAliases(body map[string]interface{}) {
	if images := collectStringFields(body, "image_urls", "images", "image", "input_reference"); len(images) > 0 {
		body["image_urls"] = images
	}
	if videos := collectStringFields(body, "video_urls", "videos", "video", "video_url"); len(videos) > 0 {
		body["video_urls"] = videos
	}
	if audios := collectStringFields(body, "audio_urls", "audios", "audio", "audio_url"); len(audios) > 0 {
		body["audio_urls"] = audios
	}

	for _, field := range []string{
		"images", "image", "input_reference",
		"videos", "video", "video_url",
		"audios", "audio", "audio_url",
	} {
		delete(body, field)
	}
}

func applyVeoReferenceImages(body map[string]interface{}) {
	images := collectVeoImages(body)
	if len(images) > 2 {
		body["Ingredients_images"] = images
		delete(body, "images")
	} else if len(images) > 0 {
		body["images"] = images
		delete(body, "Ingredients_images")
	}
	delete(body, "image")
	delete(body, "input_reference")
}

func collectVeoImages(body map[string]interface{}) []string {
	return collectStringFields(body, "Ingredients_images", "images", "image", "input_reference")
}

func collectStringFields(body map[string]interface{}, fields ...string) []string {
	if body == nil {
		return nil
	}
	values := make([]string, 0)
	appendValue := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		values = append(values, value)
	}
	appendValues := func(value any) {
		switch typed := value.(type) {
		case []string:
			for _, item := range typed {
				appendValue(item)
			}
		case []any:
			for _, item := range typed {
				if stringValue, ok := item.(string); ok {
					appendValue(stringValue)
				}
			}
		case string:
			appendValue(typed)
		}
	}
	for _, field := range fields {
		appendValues(body[field])
	}
	return values
}
