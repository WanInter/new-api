package sora

import (
	"mime/multipart"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const tokenStackHostname = "tokenstack.cc"

var tokenStackRequestFields = map[string]bool{
	"model":   true,
	"prompt":  true,
	"images":  true,
	"videos":  true,
	"audios":  true,
	"seconds": true,
	"size":    true,
}

func isTokenStackChannel(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(info.ChannelBaseUrl))
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	return hostname == tokenStackHostname || hostname == "www."+tokenStackHostname
}

func applyTokenStackJSONRequest(body map[string]interface{}) {
	if body == nil {
		return
	}

	if seconds, ok := normalizeVideoSeconds(body["seconds"]); ok {
		body["seconds"] = seconds
	} else if seconds, ok := normalizeVideoSeconds(body["duration"]); ok {
		body["seconds"] = seconds
	}

	if strings.TrimSpace(stringValue(body["size"])) == "" {
		if size := tokenStackSizeFromLegacyFields(body); size != "" {
			body["size"] = size
		}
	}

	for key := range body {
		if !tokenStackRequestFields[key] {
			delete(body, key)
		}
	}
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

func applySoraModelJSONProfile(body map[string]interface{}, profile soraModelProfile) {
	switch profile.JSONTransform {
	case requestTransformOpenAIContent:
		applyContentRequestRules(body, profile.ContentRules)
	case requestTransformOtoySeedanceReference:
		applyOtoySeedanceMiniReferenceRequest(body)
	case requestTransformVeoReferenceImages:
		applyVeoReferenceImages(body)
	}
	if profile.DropSecondsField {
		delete(body, "seconds")
	}
}

func applySoraModelMultipartProfile(writer *multipart.Writer, values map[string][]string, profile soraModelProfile) {
	if profile.MultipartTransform == requestTransformOtoySeedanceReference {
		writeOtoySeedanceMiniReferenceMultipartFields(writer, values)
	}
}

func applyContentRequestRules(body map[string]interface{}, rules *contentRequestRules) {
	if body == nil || rules == nil || !rules.ConvertLegacyMedia {
		return
	}

	if !hasContentItems(body["content"]) {
		content := make([]interface{}, 0)
		content = appendLegacyURLContent(content, "image_url", body["images"])
		content = appendLegacyURLContent(content, "image_url", body["image"])
		content = appendLegacyURLContent(content, "image_url", body["image_urls"])
		content = appendLegacyURLContent(content, "image_url", body["input_reference"])
		content = appendLegacyURLContent(content, "image_url", nestedRequestValue(body, "input", "start_frames"))
		content = appendLegacyURLContent(content, "image_url", nestedRequestValue(body, "input", "image_references"))
		content = appendLegacyURLContent(content, "image_url", nestedRequestValue(body, "metadata", "start_frames"))
		content = appendLegacyURLContent(content, "video_url", body["video"])
		content = appendLegacyURLContent(content, "video_url", body["videos"])
		content = appendLegacyURLContent(content, "video_url", body["video_url"])
		content = appendLegacyURLContent(content, "video_url", body["video_urls"])
		content = appendLegacyURLContent(content, "audio_url", body["audio"])
		content = appendLegacyURLContent(content, "audio_url", body["audios"])
		content = appendLegacyURLContent(content, "audio_url", body["audio_url"])
		content = appendLegacyURLContent(content, "audio_url", body["audio_urls"])
		if prompt, ok := body["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": prompt,
			})
		}
		body["content"] = content
	}

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

	allowedPassthrough := map[string]bool{
		"prompt":         true,
		"type":           true,
		"video_urls":     true,
		"audio_urls":     true,
		"resolution":     true,
		"aspect_ratio":   true,
		"generate_audio": true,
		"end_user_id":    true,
	}
	for key, values := range values {
		if allowedPassthrough[key] {
			writeValues(key, values)
		}
	}

	if len(values["aspect_ratio"]) == 0 {
		writeValues("aspect_ratio", values["ratio"])
	}
	if duration, ok := normalizeVideoDurationString(firstFormValue(values, "duration")); ok {
		_ = writer.WriteField("duration", duration)
	} else if duration, ok := normalizeVideoDurationString(firstFormValue(values, "seconds")); ok {
		_ = writer.WriteField("duration", duration)
	}

	imageValues := append([]string{}, values["image_urls"]...)
	imageValues = append(imageValues, values["images"]...)
	imageValues = append(imageValues, values["image"]...)
	imageValues = append(imageValues, values["input_reference"]...)
	imageValues = append(imageValues, values["file_paths"]...)
	writeValues("image_urls", uniqueStrings(imageValues))

	if len(values["type"]) == 0 {
		_ = writer.WriteField("type", "image-to-video")
	}
	if len(values["generate_audio"]) == 0 {
		_ = writer.WriteField("generate_audio", "true")
	}
}

func firstFormValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func applyOtoySeedanceMiniReferenceRequest(body map[string]interface{}) {
	if body == nil {
		return
	}

	if _, exists := body["aspect_ratio"]; !exists {
		if ratio, ok := body["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
			body["aspect_ratio"] = strings.TrimSpace(ratio)
		}
	}

	delete(body, "seconds")
	if duration, ok := normalizeVideoDurationString(body["duration"]); ok {
		body["duration"] = duration
	}

	images := collectOtoySeedanceMiniReferenceImages(body)
	if len(images) > 0 {
		body["image_urls"] = images
	}

	if _, exists := body["type"]; !exists {
		body["type"] = "image-to-video"
	}
	if _, exists := body["generate_audio"]; !exists {
		body["generate_audio"] = true
	}

	keep := map[string]bool{
		"model":          true,
		"prompt":         true,
		"type":           true,
		"image_urls":     true,
		"video_urls":     true,
		"audio_urls":     true,
		"resolution":     true,
		"duration":       true,
		"aspect_ratio":   true,
		"generate_audio": true,
		"end_user_id":    true,
	}
	for key := range body {
		if !keep[key] {
			delete(body, key)
		}
	}
}

func normalizeVideoDurationString(value any) (string, bool) {
	if duration, ok := normalizeVideoSeconds(value); ok {
		return duration, true
	}
	if stringValue, ok := value.(string); ok {
		stringValue = strings.TrimSpace(stringValue)
		if strings.EqualFold(stringValue, "auto") {
			return "auto", true
		}
	}
	return "", false
}

func collectOtoySeedanceMiniReferenceImages(body map[string]interface{}) []string {
	return collectUniqueStringFields(body, "image_urls", "images", "image", "input_reference", "file_paths")
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
	return collectUniqueStringFields(body, "Ingredients_images", "images", "image", "input_reference")
}

func collectUniqueStringFields(body map[string]interface{}, fields ...string) []string {
	if body == nil {
		return nil
	}
	values := make([]string, 0)
	seen := make(map[string]bool)
	appendValue := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
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
