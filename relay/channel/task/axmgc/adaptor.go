package axmgc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const (
	ChannelName           = "axmgc"
	DefaultBaseURL        = "https://axmgc.com"
	Seedance720p933Model  = "seedance-2-720p-933"
	defaultDuration       = 15
	defaultResolution     = "720p"
	maxImages             = 9
	maxVideos             = 3
	maxAudios             = 3
	jsonRequestContextKey = "axmgc_json_request"
)

var ModelList = []string{Seedance720p933Model}

type axmgcJSONRequest struct {
	Model           string           `json:"model"`
	Content         []map[string]any `json:"content"`
	AspectRatio     string           `json:"aspect_ratio,omitempty"`
	Resolution      string           `json:"resolution,omitempty"`
	Duration        int              `json:"duration"`
	GenerateAudio   *bool            `json:"generate_audio,omitempty"`
	Seed            *int             `json:"seed,omitempty"`
	Watermark       *bool            `json:"watermark,omitempty"`
	ReturnLastFrame *bool            `json:"return_last_frame,omitempty"`
}

type resource struct {
	Type string `json:"resource_type"`
	URL  string `json:"resource_url"`
}

type responseTask struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	Model        string     `json:"model"`
	Status       string     `json:"status"`
	ResourceList []resource `json:"resource_list"`
	FailReason   string     `json:"fail_reason"`
	Message      string     `json:"message"`
	Error        any        `json:"error"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info != nil && info.Action == constant.TaskActionRemix {
		return service.TaskErrorWrapperLocal(errors.New("Axmgc does not support video remix"), "unsupported_action", http.StatusBadRequest)
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return service.TaskErrorWrapperLocal(errors.New("Axmgc only supports JSON content with public URLs or asset IDs"), "unsupported_media_type", http.StatusUnsupportedMediaType)
	}
	if !strings.HasPrefix(contentType, "application/json") {
		return service.TaskErrorWrapperLocal(errors.New("Axmgc requires application/json"), "unsupported_media_type", http.StatusUnsupportedMediaType)
	}
	return a.validateJSONRequest(c, info)
}

func (a *TaskAdaptor) validateJSONRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var raw relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	var input map[string]any
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	content, prompt, images, videos, audios, err := validateJSONContent(input["content"], raw)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if prompt == "" && strings.TrimSpace(raw.Prompt) != "" {
		prompt = strings.TrimSpace(raw.Prompt)
		content = append(content, map[string]any{"type": "text", "text": prompt})
	}
	if prompt == "" {
		return service.TaskErrorWrapperLocal(errors.New("content must contain a non-empty text item"), "invalid_request", http.StatusBadRequest)
	}
	if err := validateResolution(raw.Resolution); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(raw.Model) == "" {
		return service.TaskErrorWrapperLocal(errors.New("model field is required"), "missing_model", http.StatusBadRequest)
	}

	raw.Model = strings.TrimSpace(raw.Model)
	raw.Prompt = prompt
	raw.AspectRatio = strings.TrimSpace(raw.AspectRatio)
	raw.Resolution = strings.TrimSpace(raw.Resolution)
	raw.Duration = defaultDuration
	storeValidatedTaskRequest(c, info, raw, images+videos+audios > 0)

	payload := axmgcJSONRequest{
		Model:       raw.Model,
		Content:     content,
		AspectRatio: raw.AspectRatio,
		Resolution:  firstNonEmpty(raw.Resolution, defaultResolution),
		Duration:    defaultDuration,
	}
	if payload.GenerateAudio, err = optionalJSONBool(input, "generate_audio"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if payload.Seed, err = optionalJSONInt(input, "seed"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if payload.Watermark, err = optionalJSONBool(input, "watermark"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if payload.ReturnLastFrame, err = optionalJSONBool(input, "return_last_frame"); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	c.Set(jsonRequestContextKey, payload)
	return nil
}

func storeValidatedTaskRequest(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq, hasReferences bool) {
	info.Action = constant.TaskActionTextGenerate
	if hasReferences {
		info.Action = constant.TaskActionGenerate
	}
	c.Set("task_request", req)
}

func contentFromURLs(raw relaycommon.TaskSubmitReq) []map[string]any {
	content := make([]map[string]any, 0, len(raw.Images)+len(raw.ImageURLs)+len(raw.Videos)+len(raw.VideoURLs)+len(raw.Audios)+len(raw.AudioURLs))
	for _, url := range appendNonEmpty([]string{raw.Image}, raw.Images, raw.ImageURLs, raw.InputStartFrames, raw.InputImageReferences) {
		content = append(content, urlContentItem("image_url", url))
	}
	for _, url := range appendNonEmpty(raw.Videos, raw.VideoURLs) {
		content = append(content, urlContentItem("video_url", url))
	}
	for _, url := range appendNonEmpty(raw.Audios, raw.AudioURLs) {
		content = append(content, urlContentItem("audio_url", url))
	}
	return content
}

func appendNonEmpty(groups ...[]string) []string {
	values := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func validateJSONContent(rawContent any, legacy relaycommon.TaskSubmitReq) ([]map[string]any, string, int, int, int, error) {
	if rawContent == nil {
		return contentFromURLs(legacy), "", len(appendNonEmpty([]string{legacy.Image}, legacy.Images, legacy.ImageURLs, legacy.InputStartFrames, legacy.InputImageReferences)), len(appendNonEmpty(legacy.Videos, legacy.VideoURLs)), len(appendNonEmpty(legacy.Audios, legacy.AudioURLs)), nil
	}
	items, ok := rawContent.([]any)
	if !ok {
		return nil, "", 0, 0, 0, errors.New("content must be an array")
	}
	content := make([]map[string]any, 0, len(items))
	prompts := make([]string, 0, 1)
	images, videos, audios := 0, 0, 0
	seenText := false
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, "", 0, 0, 0, errors.New("content items must be objects")
		}
		normalized, category, text, err := normalizeJSONContentItem(item)
		if err != nil {
			return nil, "", 0, 0, 0, err
		}
		if category != "" && seenText {
			return nil, "", 0, 0, 0, errors.New("reference content must appear before text content")
		}
		switch category {
		case "image":
			images++
		case "video":
			videos++
		case "audio":
			audios++
		case "text":
			seenText = true
			prompts = append(prompts, text)
		}
		content = append(content, normalized)
	}
	if err := validateMediaCounts(images, videos, audios); err != nil {
		return nil, "", 0, 0, 0, err
	}
	return content, strings.Join(prompts, "\n"), images, videos, audios, nil
}

func normalizeJSONContentItem(item map[string]any) (map[string]any, string, string, error) {
	contentType, ok := item["type"].(string)
	if !ok || strings.TrimSpace(contentType) == "" {
		return nil, "", "", errors.New("content.type is required")
	}
	contentType = strings.TrimSpace(contentType)
	switch contentType {
	case "text":
		text, _ := item["text"].(string)
		if text = strings.TrimSpace(text); text == "" {
			return nil, "", "", errors.New("text content must be non-empty")
		}
		return map[string]any{"type": contentType, "text": text}, "text", text, nil
	case "image_url", "video_url", "audio_url":
		url, err := contentURL(item, contentType)
		if err != nil {
			return nil, "", "", err
		}
		return urlContentItem(contentType, url), strings.TrimSuffix(contentType, "_url"), "", nil
	case "image_asset", "video_asset", "audio_asset":
		assetID, err := contentAssetID(item, contentType)
		if err != nil {
			return nil, "", "", err
		}
		return map[string]any{"type": contentType, contentType: map[string]any{"asset_id": assetID}}, strings.TrimSuffix(contentType, "_asset"), "", nil
	default:
		return nil, "", "", fmt.Errorf("unsupported content type %q", contentType)
	}
}

func contentURL(item map[string]any, field string) (string, error) {
	value, ok := item[field]
	if !ok {
		value = item["url"]
	}
	switch typed := value.(type) {
	case string:
		if url := strings.TrimSpace(typed); url != "" {
			return url, nil
		}
	case map[string]any:
		if rawURL, ok := typed["url"].(string); ok {
			if url := strings.TrimSpace(rawURL); url != "" {
				return url, nil
			}
		}
	}
	return "", fmt.Errorf("%s.url is required", field)
}

func contentAssetID(item map[string]any, field string) (string, error) {
	asset, ok := item[field].(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s.asset_id is required", field)
	}
	assetID, _ := asset["asset_id"].(string)
	if assetID = strings.TrimSpace(assetID); assetID == "" {
		return "", fmt.Errorf("%s.asset_id is required", field)
	}
	return assetID, nil
}

func urlContentItem(contentType, url string) map[string]any {
	return map[string]any{"type": contentType, contentType: map[string]any{"url": url}}
}

func optionalJSONBool(input map[string]any, field string) (*bool, error) {
	value, ok := input[field]
	if !ok {
		return nil, nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result bool
	if err := common.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("%s must be a boolean", field)
	}
	return &result, nil
}

func optionalJSONInt(input map[string]any, field string) (*int, error) {
	value, ok := input[field]
	if !ok {
		return nil, nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result int
	if err := common.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("%s must be an integer", field)
	}
	return &result, nil
}

func validateMediaCounts(images, videos, audios int) error {
	if images > maxImages {
		return fmt.Errorf("at most %d images are supported", maxImages)
	}
	if videos > maxVideos {
		return fmt.Errorf("at most %d videos are supported", maxVideos)
	}
	if audios > maxAudios {
		return fmt.Errorf("at most %d audios are supported", maxAudios)
	}
	return nil
}

func validateResolution(resolution string) error {
	if resolution = strings.TrimSpace(resolution); resolution != "" && !strings.EqualFold(resolution, defaultResolution) {
		return fmt.Errorf("resolution must be %s for %s", defaultResolution, Seedance720p933Model)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info != nil && info.Action == constant.TaskActionRemix {
		return "", errors.New("Axmgc does not support video remix")
	}
	return a.baseURL + "/v1/video/generations", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey := strings.TrimSpace(c.GetHeader("X-Idempotency-Key")); idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	payload, ok := c.Get(jsonRequestContextKey)
	if !ok {
		return nil, errors.New("validated Axmgc JSON request is unavailable")
	}
	request, ok := payload.(axmgcJSONRequest)
	if !ok {
		return nil, errors.New("invalid Axmgc JSON request")
	}
	request.Model = upstreamModelName(info, request.Model)
	if request.Model == "" {
		return nil, errors.New("Axmgc upstream model is required")
	}
	data, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	c.Request.Header.Set("Content-Type", "application/json")
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) NormalizeBillingRequestBody(_ *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var request map[string]any
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	request["duration"] = defaultDuration
	delete(request, "seconds")
	return common.Marshal(request)
}

func upstreamModelName(info *relaycommon.RelayInfo, fallback string) string {
	if info != nil && info.IsModelMapped {
		if name := strings.TrimSpace(info.UpstreamModelName); name != "" {
			return name
		}
	}
	return strings.TrimSpace(fallback)
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var parsed responseTask
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamTaskID := firstNonEmpty(parsed.ID, parsed.TaskID)
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("Axmgc response has no task id"), "invalid_response", http.StatusBadGateway)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Model = info.OriginModelName
	status := mapStatus(parsed.Status)
	if status == model.TaskStatusUnknown {
		status = model.TaskStatusSubmitted
	}
	video.Status = status.ToVideoStatus()
	video.CreatedAt = time.Now().Unix()
	c.JSON(http.StatusOK, video)
	return upstreamTaskID, body, nil
}

func (a *TaskAdaptor) BuildPrivateData(_ *gin.Context, info *relaycommon.RelayInfo) (*model.TaskPrivateData, error) {
	if info == nil || info.ChannelMeta == nil || strings.TrimSpace(info.ApiKey) == "" {
		return nil, errors.New("Axmgc selected API key is unavailable")
	}
	return &model.TaskPrivateData{Key: info.ApiKey}, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if taskID = strings.TrimSpace(taskID); taskID == "" {
		return nil, errors.New("invalid task_id")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/video/generations/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var parsed responseTask
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal Axmgc task result failed")
	}
	status := mapStatus(parsed.Status)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown Axmgc task status %q", parsed.Status)
	}
	result := &relaycommon.TaskInfo{
		Code:   0,
		TaskID: firstNonEmpty(parsed.ID, parsed.TaskID),
		Status: string(status),
	}
	switch status {
	case model.TaskStatusSuccess:
		result.Progress = "100%"
		result.Url = extractVideoURL(parsed.ResourceList)
		if result.Url == "" {
			return nil, errors.New("Axmgc succeeded task has no video resource_url")
		}
	case model.TaskStatusFailure:
		result.Progress = "100%"
		result.Reason = firstNonEmpty(parsed.FailReason, errorMessage(parsed.Error), parsed.Message, "Axmgc task failed")
	case model.TaskStatusSubmitted:
		result.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		result.Progress = taskcommon.ProgressQueued
	default:
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

func mapStatus(raw string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending", "queued":
		return model.TaskStatusQueued
	case "submitted":
		return model.TaskStatusSubmitted
	case "running", "processing", "in_progress":
		return model.TaskStatusInProgress
	case "succeeded", "success", "completed", "complete", "done":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusUnknown
	}
}

func extractVideoURL(resources []resource) string {
	for _, resource := range resources {
		if strings.EqualFold(strings.TrimSpace(resource.Type), "video") && strings.TrimSpace(resource.URL) != "" {
			return strings.TrimSpace(resource.URL)
		}
	}
	for _, resource := range resources {
		if strings.TrimSpace(resource.URL) != "" {
			return strings.TrimSpace(resource.URL)
		}
	}
	return ""
}

func errorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if message, ok := typed["message"].(string); ok {
			return strings.TrimSpace(message)
		}
	}
	return ""
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := dto.NewOpenAIVideo()
	video.ID = task.TaskID
	video.TaskID = task.TaskID
	video.Model = task.Properties.OriginModelName
	video.Status = task.Status.ToVideoStatus()
	video.SetProgressStr(task.Progress)
	video.CreatedAt = task.CreatedAt
	video.CompletedAt = task.CompletionTime()
	if url := strings.TrimSpace(task.GetResultURL()); url != "" {
		video.SetMetadata("url", url)
		video.SetMetadata("video_url", url)
		video.SetMetadata("result_url", url)
	}
	if task.Status == model.TaskStatusFailure {
		video.Error = &dto.OpenAIVideoError{Message: task.FailReason, Code: "failure"}
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
