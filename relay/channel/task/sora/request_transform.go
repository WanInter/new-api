package sora

import (
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"strconv"
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

func applySoraModelJSONProfile(body map[string]interface{}, profile soraModelProfile) error {
	if profile.DropSecondsField && profile.JSONTransform != requestTransformGrokImageVideo && profile.JSONTransform != requestTransformGrokVideo15 {
		moveSecondsFieldToDuration(body)
	}

	switch profile.JSONTransform {
	case requestTransformOpenAIContent:
		applyOpenAIContentRequest(body)
	case requestTransformOtoySeedanceReference:
		applyOtoySeedanceMiniReferenceRequest(body)
	case requestTransformVeoReferenceImages:
		applyVeoReferenceImages(body)
	case requestTransformGrokImageVideo:
		return applyGrokImageVideoRequest(body)
	case requestTransformGrokVideo15:
		return applyGrokVideo15Request(body)
	}
	return nil
}

func applySoraModelJSONFinalProfile(body map[string]interface{}, profile soraModelProfile) {
	if profile.JSONFinalTransform == requestTransformTokenStackSora15s {
		applyTokenStackJSONRequest(body)
	}
}

func applySoraModelMultipartProfile(writer *multipart.Writer, values map[string][]string, profile soraModelProfile) error {
	switch profile.MultipartTransform {
	case requestTransformOtoySeedanceReference:
		writeOtoySeedanceMiniReferenceMultipartFields(writer, values)
	case requestTransformGrokImageVideo:
		return writeGrokMultipartFields(writer, values, true)
	case requestTransformGrokVideo15:
		return writeGrokMultipartFields(writer, values, false)
	}
	return nil
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

type grokRequestInput struct {
	Images                []string
	Videos                []string
	Audios                []string
	Mode                  string
	HasExplicitFirstFrame bool
	Duration              int
	HasDuration           bool
	AspectRatio           string
	Resolution            string
	Size                  string
}

func applyGrokImageVideoRequest(body map[string]interface{}) error {
	input, err := grokRequestInputFromBody(body)
	if err != nil {
		return err
	}
	mode, err := validateGrokImageVideoRequest(input)
	if err != nil {
		return err
	}

	applyGrokImageURLs(body, input.Images)
	body["mode"] = mode
	body["duration"] = input.Duration
	body["aspect_ratio"] = input.AspectRatio
	body["resolution"] = input.Resolution
	delete(body, "seconds")
	delete(body, "ratio")
	delete(body, "aspectRatio")
	delete(body, "size")
	return nil
}

func applyGrokVideo15Request(body map[string]interface{}) error {
	input, err := grokRequestInputFromBody(body)
	if err != nil {
		return err
	}
	size, err := validateGrokVideo15Request(input)
	if err != nil {
		return err
	}

	applyGrokImageURLs(body, input.Images)
	delete(body, "mode")
	delete(body, "seconds")
	delete(body, "resolution")
	delete(body, "ratio")
	delete(body, "aspectRatio")
	if input.HasDuration {
		body["duration"] = input.Duration
	} else {
		delete(body, "duration")
	}
	if input.AspectRatio != "" {
		body["aspect_ratio"] = input.AspectRatio
	} else {
		delete(body, "aspect_ratio")
	}
	if size != "" {
		body["size"] = size
	} else {
		delete(body, "size")
	}
	return nil
}

func grokRequestInputFromBody(body map[string]interface{}) (grokRequestInput, error) {
	if body == nil {
		return grokRequestInput{}, fmt.Errorf("grok request body is required")
	}

	data, err := common.Marshal(body)
	if err != nil {
		return grokRequestInput{}, err
	}
	var req relaycommon.TaskSubmitReq
	if err := common.Unmarshal(data, &req); err != nil {
		return grokRequestInput{}, err
	}
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return grokRequestInput{}, err
	}

	upstreamImages, err := grokStringSliceField(body, "images_url")
	if err != nil {
		return grokRequestInput{}, err
	}
	hasRawVideo, err := grokHasRawMediaInput(body, "video", "videos", "video_url", "video_urls")
	if err != nil {
		return grokRequestInput{}, err
	}
	hasRawAudio, err := grokHasRawMediaInput(body, "audio", "audios", "audio_url", "audio_urls")
	if err != nil {
		return grokRequestInput{}, err
	}
	duration, hasDuration, err := grokRequestDuration(body)
	if err != nil {
		return grokRequestInput{}, err
	}
	videos := grokVideoURLs(&req)
	if hasRawVideo && len(videos) == 0 {
		videos = []string{"provided"}
	}
	audios := grokAudioURLs(&req)
	if hasRawAudio && len(audios) == 0 {
		audios = []string{"provided"}
	}

	input := grokRequestInput{
		Images:                appendNonEmptySoraURLs(grokImageURLs(&req), upstreamImages...),
		Videos:                videos,
		Audios:                audios,
		Mode:                  strings.TrimSpace(req.Mode),
		HasExplicitFirstFrame: grokHasExplicitFirstFrame(&req),
		Duration:              duration,
		HasDuration:           hasDuration,
		AspectRatio:           strings.TrimSpace(req.AspectRatio),
		Resolution:            strings.TrimSpace(req.Resolution),
		Size:                  strings.TrimSpace(req.Size),
	}
	return input, nil
}

func grokStringSliceField(body map[string]interface{}, field string) ([]string, error) {
	value, exists := body[field]
	if !exists || value == nil {
		return nil, nil
	}

	values := make([]string, 0)
	appendValue := func(value string) {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	switch typed := value.(type) {
	case string:
		appendValue(typed)
	case []string:
		for _, item := range typed {
			appendValue(item)
		}
	case []interface{}:
		for _, item := range typed {
			itemValue, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string or an array of strings", field)
			}
			appendValue(itemValue)
		}
	default:
		return nil, fmt.Errorf("%s must be a string or an array of strings", field)
	}
	return values, nil
}

func grokHasRawMediaInput(body map[string]interface{}, fields ...string) (bool, error) {
	for _, field := range fields {
		values, err := grokStringSliceField(body, field)
		if err != nil {
			return false, err
		}
		if len(values) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func grokRequestDuration(body map[string]interface{}) (int, bool, error) {
	duration, hasDuration, err := grokDurationField(body, "duration")
	if err != nil {
		return 0, false, err
	}
	seconds, hasSeconds, err := grokDurationField(body, "seconds")
	if err != nil {
		return 0, false, err
	}
	if hasDuration && hasSeconds && duration != seconds {
		return 0, false, fmt.Errorf("duration %d conflicts with seconds %d", duration, seconds)
	}
	metadataDuration, hasMetadataDuration, err := grokMetadataDuration(body)
	if err != nil {
		return 0, false, err
	}

	canonicalDuration := duration
	hasCanonicalDuration := hasDuration
	canonicalField := "duration"
	if !hasCanonicalDuration && hasSeconds {
		canonicalDuration = seconds
		hasCanonicalDuration = true
		canonicalField = "seconds"
	}
	if hasCanonicalDuration && hasMetadataDuration && canonicalDuration != metadataDuration {
		return 0, false, fmt.Errorf("metadata.duration %d conflicts with %s %d", metadataDuration, canonicalField, canonicalDuration)
	}
	if hasMetadataDuration {
		canonicalDuration = metadataDuration
		hasCanonicalDuration = true
		if err := removeGrokMetadataDuration(body); err != nil {
			return 0, false, err
		}
	}
	if hasCanonicalDuration {
		// Keep the parsed request, billing input, and final Grok payload on the
		// same canonical numeric duration field.
		body["duration"] = canonicalDuration
		delete(body, "seconds")
		return canonicalDuration, true, nil
	}
	return 0, false, nil
}

func grokMetadataDuration(body map[string]interface{}) (int, bool, error) {
	metadata, ok := grokMetadataMap(body)
	if !ok {
		return 0, false, nil
	}
	duration, hasDuration, err := grokDurationField(metadata, "duration")
	if err != nil {
		return 0, false, fmt.Errorf("metadata.duration must be a positive integer")
	}
	return duration, hasDuration, nil
}

func removeGrokMetadataDuration(body map[string]interface{}) error {
	metadata, ok := grokMetadataMap(body)
	if !ok {
		return nil
	}
	delete(metadata, "duration")
	if len(metadata) == 0 {
		delete(body, "metadata")
		return nil
	}
	if _, encoded := body["metadata"].(string); encoded {
		data, err := common.Marshal(metadata)
		if err != nil {
			return err
		}
		body["metadata"] = string(data)
	}
	return nil
}

func grokMetadataMap(body map[string]interface{}) (map[string]interface{}, bool) {
	value, exists := body["metadata"]
	if !exists || value == nil {
		return nil, false
	}
	switch metadata := value.(type) {
	case map[string]interface{}:
		return metadata, true
	case string:
		parsed := make(map[string]interface{})
		if err := common.UnmarshalJsonStr(metadata, &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}

func grokDurationField(body map[string]interface{}, field string) (int, bool, error) {
	value, exists := body[field]
	if !exists || value == nil {
		return 0, false, nil
	}
	duration, err := grokDurationValue(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a positive integer", field)
	}
	return duration, true, nil
}

func grokDurationValue(value interface{}) (int, error) {
	parse := func(value string) (int, error) {
		value = strings.TrimSpace(strings.ToLower(value))
		for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
			if strings.HasSuffix(value, suffix) {
				value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
				break
			}
		}
		duration, err := strconv.Atoi(value)
		if err != nil || duration <= 0 {
			return 0, fmt.Errorf("invalid duration")
		}
		return duration, nil
	}

	switch typed := value.(type) {
	case string:
		return parse(typed)
	case json.Number:
		return parse(string(typed))
	case int:
		if typed > 0 {
			return typed, nil
		}
	case int64:
		if typed > 0 && typed <= math.MaxInt32 {
			return int(typed), nil
		}
	case float64:
		if typed > 0 && typed <= math.MaxInt32 && typed == math.Trunc(typed) {
			return int(typed), nil
		}
	case float32:
		value := float64(typed)
		if value > 0 && value <= math.MaxInt32 && value == math.Trunc(value) {
			return int(value), nil
		}
	}
	return 0, fmt.Errorf("invalid duration")
}

func grokImageURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	urls := appendNonEmptySoraURLs(nil, req.Images...)
	urls = appendNonEmptySoraURLs(urls, req.Image)
	urls = appendNonEmptySoraURLs(urls, req.ImageURLs...)
	urls = appendNonEmptySoraURLs(urls, req.InputReference)
	urls = appendNonEmptySoraURLs(urls, req.InputStartFrames...)
	urls = appendNonEmptySoraURLs(urls, req.InputImageReferences...)
	urls = appendNonEmptySoraURLs(urls, req.MetadataStartFrames...)
	for _, item := range req.Content {
		if item.ImageURL != nil {
			urls = appendNonEmptySoraURLs(urls, item.ImageURL.URL)
		}
	}
	return urls
}

func grokHasExplicitFirstFrame(req *relaycommon.TaskSubmitReq) bool {
	if req == nil {
		return false
	}
	if len(req.InputStartFrames) > 0 || len(req.MetadataStartFrames) > 0 {
		return true
	}
	for _, item := range req.Content {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if item.ImageURL != nil && (role == "first_frame" || role == "first-frame" || role == "start_frame") {
			return true
		}
	}
	return false
}

func grokVideoURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	urls := appendNonEmptySoraURLs(nil, req.Videos...)
	urls = appendNonEmptySoraURLs(urls, req.VideoURLs...)
	for _, item := range req.Content {
		if item.VideoURL != nil {
			urls = appendNonEmptySoraURLs(urls, item.VideoURL.URL)
		}
	}
	return urls
}

func grokAudioURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	urls := appendNonEmptySoraURLs(nil, req.Audios...)
	urls = appendNonEmptySoraURLs(urls, req.AudioURLs...)
	for _, item := range req.Content {
		if item.AudioURL != nil {
			urls = appendNonEmptySoraURLs(urls, item.AudioURL.URL)
		}
	}
	return urls
}

func appendNonEmptySoraURLs(target []string, values ...string) []string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			target = append(target, value)
		}
	}
	return target
}

func applyGrokImageURLs(body map[string]interface{}, images []string) {
	if len(images) > 0 {
		body["images_url"] = images
	} else {
		delete(body, "images_url")
	}
	for _, field := range []string{
		"images", "image", "image_urls", "input_reference",
		"video", "videos", "video_url", "video_urls",
		"audio", "audios", "audio_url", "audio_urls",
		"content",
	} {
		delete(body, field)
	}
	deleteNestedRequestFields(body, "input", "start_frames", "image_references")
	deleteNestedRequestFields(body, "metadata", "start_frames")
}

func resolveGrokImageVideoMode(value string, imageCount int, hasExplicitFirstFrame bool) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		switch {
		case imageCount == 0:
			return "text", nil
		case hasExplicitFirstFrame:
			if imageCount != 1 {
				return "", fmt.Errorf("grok-image-video mode frame requires exactly 1 image input, got %d", imageCount)
			}
			return "frame", nil
		case imageCount <= 7:
			return "ref", nil
		default:
			return "", fmt.Errorf("grok-image-video supports at most 7 image inputs, got %d", imageCount)
		}
	}

	switch mode {
	case "text":
		if imageCount != 0 {
			return "", fmt.Errorf("grok-image-video mode text does not accept image inputs")
		}
	case "frame":
		if imageCount != 1 {
			return "", fmt.Errorf("grok-image-video mode frame requires exactly 1 image input, got %d", imageCount)
		}
	case "ref":
		if imageCount < 1 || imageCount > 7 {
			return "", fmt.Errorf("grok-image-video mode ref requires 1 to 7 image inputs, got %d", imageCount)
		}
	default:
		return "", fmt.Errorf("grok-image-video mode must be one of: text, frame, ref")
	}
	return mode, nil
}

func validateGrokImageVideoRequest(input grokRequestInput) (string, error) {
	if len(input.Videos) > 0 || len(input.Audios) > 0 {
		return "", fmt.Errorf("grok-image-video supports image inputs only and does not support video or audio references")
	}
	if input.Size != "" {
		return "", fmt.Errorf("grok-image-video does not support size; use resolution")
	}
	if !input.HasDuration {
		return "", fmt.Errorf("grok-image-video duration is required and must be one of: 6, 10, 15")
	}
	if !isGrokImageVideoDuration(input.Duration) {
		return "", fmt.Errorf("grok-image-video duration must be one of: 6, 10, 15")
	}
	if !isGrokImageVideoAspectRatio(input.AspectRatio) {
		return "", fmt.Errorf("grok-image-video aspect_ratio is required and must be one of: 16:9, 9:16, 1:1")
	}
	if !isGrokResolution(input.Resolution) {
		return "", fmt.Errorf("grok-image-video resolution is required and must be one of: 480p, 720p")
	}
	return resolveGrokImageVideoMode(input.Mode, len(input.Images), input.HasExplicitFirstFrame)
}

func validateGrokVideo15Request(input grokRequestInput) (string, error) {
	if len(input.Videos) > 0 || len(input.Audios) > 0 {
		return "", fmt.Errorf("grok-video-1.5 supports exactly one image input and does not support video or audio references")
	}
	if len(input.Images) != 1 {
		return "", fmt.Errorf("grok-video-1.5 requires exactly 1 image input, got %d", len(input.Images))
	}
	if input.Mode != "" {
		return "", fmt.Errorf("grok-video-1.5 does not support mode")
	}
	if input.HasDuration && (input.Duration < 1 || input.Duration > 15) {
		return "", fmt.Errorf("grok-video-1.5 duration must be an integer from 1 to 15")
	}
	if input.AspectRatio != "" && !isGrokVideo15AspectRatio(input.AspectRatio) {
		return "", fmt.Errorf("grok-video-1.5 does not support aspect_ratio %q", input.AspectRatio)
	}
	return grokVideo15OutputSize(input)
}

func grokVideo15OutputSize(input grokRequestInput) (string, error) {
	size := strings.ToLower(strings.TrimSpace(input.Size))
	resolution := strings.ToLower(strings.TrimSpace(input.Resolution))
	if size != "" && resolution != "" && size != resolution {
		return "", fmt.Errorf("grok-video-1.5 size %q conflicts with resolution %q", input.Size, input.Resolution)
	}
	output := firstNonEmpty(resolution, size)
	if output == "" {
		return "", nil
	}
	if output != "480p" && output != "720p" {
		return "", fmt.Errorf("grok-video-1.5 supports size 480p or 720p, got %q", output)
	}
	return output, nil
}

func isGrokImageVideoDuration(value int) bool {
	switch value {
	case 6, 10, 15:
		return true
	default:
		return false
	}
}

func isGrokImageVideoAspectRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "1:1":
		return true
	default:
		return false
	}
}

func isGrokResolution(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "480p" || value == "720p"
}

func isGrokVideo15AspectRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "1:1", "4:3", "3:4", "3:2", "2:3":
		return true
	default:
		return false
	}
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

func writeGrokMultipartFields(writer *multipart.Writer, values map[string][]string, imageVideo bool) error {
	input, err := grokRequestInputFromMultipartValues(values)
	if err != nil {
		return err
	}

	mode := ""
	size := ""
	if imageVideo {
		mode, err = validateGrokImageVideoRequest(input)
	} else {
		size, err = validateGrokVideo15Request(input)
	}
	if err != nil {
		return err
	}

	writeValues := func(key string, vals []string) {
		for _, value := range vals {
			if value = strings.TrimSpace(value); value != "" {
				_ = writer.WriteField(key, value)
			}
		}
	}

	transformedFields := map[string]bool{
		"model":           true,
		"duration":        true,
		"seconds":         true,
		"mode":            true,
		"images_url":      true,
		"images":          true,
		"image":           true,
		"image_urls":      true,
		"input_reference": true,
		"content":         true,
		"video":           true,
		"videos":          true,
		"video_url":       true,
		"video_urls":      true,
		"audio":           true,
		"audios":          true,
		"audio_url":       true,
		"audio_urls":      true,
		"size":            true,
		"resolution":      true,
		"aspect_ratio":    true,
		"ratio":           true,
		"aspectRatio":     true,
	}

	for key, fieldValues := range values {
		if transformedFields[key] {
			continue
		}
		switch key {
		case "input":
			writeValues(key, grokMultipartNestedValuesWithoutFields(fieldValues, "start_frames", "image_references"))
		case "metadata":
			writeValues(key, grokMultipartNestedValuesWithoutFields(fieldValues, "start_frames", "duration"))
		default:
			writeValues(key, fieldValues)
		}
	}

	if imageVideo {
		_ = writer.WriteField("mode", mode)
		_ = writer.WriteField("duration", strconv.Itoa(input.Duration))
		_ = writer.WriteField("aspect_ratio", input.AspectRatio)
		_ = writer.WriteField("resolution", input.Resolution)
	} else {
		if input.HasDuration {
			_ = writer.WriteField("duration", strconv.Itoa(input.Duration))
		}
		if input.AspectRatio != "" {
			_ = writer.WriteField("aspect_ratio", input.AspectRatio)
		}
		if size != "" {
			_ = writer.WriteField("size", size)
		}
	}
	writeValues("images_url", input.Images)
	return nil
}

func grokRequestInputFromMultipartValues(values map[string][]string) (grokRequestInput, error) {
	body := make(map[string]interface{}, len(values))
	for key, fieldValues := range values {
		switch len(fieldValues) {
		case 0:
			continue
		case 1:
			body[key] = fieldValues[0]
		default:
			body[key] = append([]string(nil), fieldValues...)
		}
	}
	return grokRequestInputFromBody(body)
}

func grokMultipartNestedValuesWithoutFields(values []string, fields ...string) []string {
	transformed := make([]string, 0, len(values))
	for _, value := range values {
		nested := make(map[string]interface{})
		if err := common.UnmarshalJsonStr(value, &nested); err != nil {
			transformed = append(transformed, value)
			continue
		}
		for _, field := range fields {
			delete(nested, field)
		}
		if len(nested) == 0 {
			continue
		}
		encoded, err := common.Marshal(nested)
		if err != nil {
			transformed = append(transformed, value)
			continue
		}
		transformed = append(transformed, string(encoded))
	}
	return transformed
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
	if len(values["resolution"]) > 0 {
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
	if strings.TrimSpace(stringValue(body["resolution"])) != "" {
		return
	}
	aspectRatio, resolution, ok := otoyAspectRatioAndResolutionFromSize(stringValue(body["size"]))
	if !ok {
		return
	}
	if strings.TrimSpace(stringValue(body["aspect_ratio"])) == "" {
		body["aspect_ratio"] = aspectRatio
	}
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
