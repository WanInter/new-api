package yoboxcorp

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
	ChannelName    = "yoboxcorp"
	DefaultBaseURL = "https://corp.yoboxai.com"

	generatePath = "/v1/video/generate"
	taskPath     = "/v1/video/tasks"
)

var ModelList = []string{
	"dreamina-seedance-2-0-hc",
	"dreamina-seedance-2-0-fast-hc",
	"dreamina-seedance-2-0-mini-hc",
}

type generateResponse struct {
	Task *upstreamTask `json:"task"`
}

type upstreamTask struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Model   string   `json:"model"`
	Outputs []string `json:"outputs"`
	Error   any      `json:"error"`
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
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = info.OriginModelName
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(errors.New("model field is required"), "missing_model", http.StatusBadRequest)
	}
	mergeRequestOptions(c, &req)
	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(requestDuration(req))}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + generatePath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	payload := buildGeneratePayload(req, info)
	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var parsed generateResponse
	if err := common.Unmarshal(responseBody, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if parsed.Task == nil || strings.TrimSpace(parsed.Task.ID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("YoboxCorp generate response has no task id"), "invalid_response", http.StatusBadGateway)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	video.Status = model.TaskStatus(model.TaskStatusSubmitted).ToVideoStatus()
	c.JSON(http.StatusOK, video)
	return parsed.Task.ID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task_id")
	}
	uri := strings.TrimRight(baseURL, "/") + taskPath + "/" + url.PathEscape(taskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
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

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var parsed generateResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	if parsed.Task == nil {
		return nil, errors.New("YoboxCorp task response has no task")
	}
	status := mapTaskStatus(parsed.Task.Status)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown YoboxCorp task status %q", parsed.Task.Status)
	}

	result := &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   parsed.Task.ID,
		Status:   string(status),
		Progress: progressForStatus(status),
	}
	if status == model.TaskStatusSuccess {
		result.Url = firstString(parsed.Task.Outputs)
		result.Progress = taskcommon.ProgressComplete
	}
	if status == model.TaskStatusFailure {
		result.Reason = taskErrorMessage(parsed.Task.Error)
		if result.Reason == "" {
			result.Reason = "task failed"
		}
		result.Progress = taskcommon.ProgressComplete
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	if resultURL := strings.TrimSpace(task.GetResultURL()); resultURL != "" {
		video.SetMetadata("url", resultURL)
		video.SetMetadata("video_url", resultURL)
		video.SetMetadata("result_url", resultURL)
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) BuildPrivateData(_ *gin.Context, info *relaycommon.RelayInfo) (*model.TaskPrivateData, error) {
	if info == nil {
		return nil, errors.New("relay info is nil")
	}
	return &model.TaskPrivateData{Key: info.ApiKey}, nil
}

func buildGeneratePayload(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) map[string]any {
	return map[string]any{
		"model":      upstreamModelName(req, info),
		"content":    buildContent(req),
		"duration":   requestDuration(req),
		"resolution": requestResolution(req),
		"ratio":      requestRatio(req),
		"watermark":  requestWatermark(req),
	}
}

func upstreamModelName(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil {
		if modelName := strings.TrimSpace(info.UpstreamModelName); modelName != "" {
			return modelName
		}
		if modelName := strings.TrimSpace(info.OriginModelName); modelName != "" {
			return modelName
		}
	}
	return strings.TrimSpace(req.Model)
}

func buildContent(req relaycommon.TaskSubmitReq) []any {
	content := make([]any, 0, len(req.Content)+1+len(req.Images)+len(req.Videos)+len(req.Audios))
	seen := make(map[string]struct{})
	hasText := false
	for _, item := range req.Content {
		switch item.Type {
		case "text":
			if text := strings.TrimSpace(item.Text); text != "" {
				content = append(content, map[string]any{"type": "text", "text": text})
				hasText = true
			}
		case "image_url":
			if item.ImageURL != nil {
				content = appendReference(content, seen, "image_url", "reference_image", item.ImageURL.URL)
			}
		case "video_url":
			if item.VideoURL != nil {
				content = appendReference(content, seen, "video_url", "reference_video", item.VideoURL.URL)
			}
		case "audio_url":
			if item.AudioURL != nil {
				content = appendReference(content, seen, "audio_url", "reference_audio", item.AudioURL.URL)
			}
		}
	}
	if !hasText && strings.TrimSpace(req.Prompt) != "" {
		content = append(content, map[string]any{"type": "text", "text": strings.TrimSpace(req.Prompt)})
	}
	for _, imageURL := range appendURLs([]string{req.Image, req.InputReference}, req.Images, req.ImageURLs, req.InputStartFrames, req.InputImageReferences, req.MetadataStartFrames) {
		content = appendReference(content, seen, "image_url", "reference_image", imageURL)
	}
	for _, videoURL := range appendURLs(req.Videos, req.VideoURLs) {
		content = appendReference(content, seen, "video_url", "reference_video", videoURL)
	}
	for _, audioURL := range appendURLs(req.Audios, req.AudioURLs) {
		content = appendReference(content, seen, "audio_url", "reference_audio", audioURL)
	}
	return content
}

func appendReference(content []any, seen map[string]struct{}, contentType, role, value string) []any {
	value = strings.TrimSpace(value)
	if value == "" {
		return content
	}
	key := contentType + "\x00" + value
	if _, exists := seen[key]; exists {
		return content
	}
	seen[key] = struct{}{}
	return append(content, map[string]any{
		"type": contentType,
		"role": role,
		contentType: map[string]any{
			"url": value,
		},
	})
}

func appendURLs(groups ...[]string) []string {
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

func requestDuration(req relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if value, ok := parseSeconds(req.Seconds); ok {
		return value
	}
	return 4
}

func requestResolution(req relaycommon.TaskSubmitReq) string {
	return firstNonEmpty(req.Resolution, metadataString(req.Metadata, "resolution"), "720p")
}

func requestRatio(req relaycommon.TaskSubmitReq) string {
	return firstNonEmpty(req.AspectRatio, metadataString(req.Metadata, "ratio"), metadataString(req.Metadata, "aspect_ratio"), ratioFromSize(req.Size), "16:9")
}

func requestWatermark(req relaycommon.TaskSubmitReq) bool {
	value, ok := req.Metadata["watermark"].(bool)
	return ok && value
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func ratioFromSize(size string) string {
	switch strings.TrimSpace(size) {
	case "720x1280":
		return "9:16"
	case "720x720":
		return "1:1"
	case "1280x720":
		return "16:9"
	default:
		return ""
	}
}

func parseSeconds(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
		value = strings.TrimSuffix(value, suffix)
	}
	value = strings.TrimSpace(value)
	var seconds int
	if _, err := fmt.Sscanf(value, "%d", &seconds); err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

func mergeRequestOptions(c *gin.Context, req *relaycommon.TaskSubmitReq) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		return
	}
	var raw map[string]any
	if common.Unmarshal(body, &raw) != nil {
		return
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	for _, key := range []string{"ratio", "aspect_ratio", "resolution", "watermark"} {
		if value, ok := raw[key]; ok {
			req.Metadata[key] = value
		}
	}
}

func mapTaskStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted":
		return model.TaskStatusSubmitted
	case "pending", "queued":
		return model.TaskStatusQueued
	case "processing", "running", "in_progress":
		return model.TaskStatusInProgress
	case "completed", "complete", "success", "succeeded", "done":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusUnknown
	}
}

func progressForStatus(status model.TaskStatus) string {
	if status == model.TaskStatusQueued || status == model.TaskStatusSubmitted {
		return "20%"
	}
	return "30%"
}

func taskErrorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return firstNonEmpty(stringFromMap(typed, "message"), stringFromMap(typed, "error"), stringFromMap(typed, "detail"))
	default:
		return ""
	}
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstString(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
