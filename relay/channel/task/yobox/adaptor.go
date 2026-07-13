package yobox

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
	defaultYoboxBaseURL              = "https://max.yoboxai.com"
	yoboxTasksPath                   = "/async/tasks"
	yoboxImageReferencesLimitMessage = "最多支持 4 张 image_references"
	yoboxGenericProcessingError      = "视频生成请求处理失败"
)

var modelList = []string{
	"seedance2",
	"seedance-2.0",
	"seedance-2.0-fast",
}

type responseTask struct {
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	TaskID     string            `json:"task_id"`
	Status     string            `json:"status"`
	Progress   int               `json:"progress"`
	FailReason string            `json:"fail_reason"`
	Data       yoboxTaskEnvelope `json:"data"`
}

type yoboxTaskEnvelope struct {
	TaskID     string           `json:"task_id"`
	Status     string           `json:"status"`
	Progress   int              `json:"progress"`
	FailReason string           `json:"fail_reason"`
	Data       yoboxTaskPayload `json:"data"`

	// Legacy/direct task payload fields kept for compatibility with older mocks.
	ID       string   `json:"id"`
	Model    string   `json:"model"`
	VideoURL string   `json:"video_url"`
	Outputs  []string `json:"outputs"`
	URL      string   `json:"url"`
	Seconds  int      `json:"seconds"`
	Phase    string   `json:"phase"`
	Error    string   `json:"error"`
}

type yoboxTaskPayload struct {
	ID         string   `json:"id"`
	Object     string   `json:"object"`
	Model      string   `json:"model"`
	Status     string   `json:"status"`
	VideoURL   string   `json:"video_url"`
	Outputs    []string `json:"outputs"`
	URL        string   `json:"url"`
	Seconds    int      `json:"seconds"`
	Phase      string   `json:"phase"`
	Error      string   `json:"error"`
	FailReason string   `json:"fail_reason"`
}

type submitResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		Action   string `json:"action"`
		Progress int    `json:"progress"`
		Platform string `json:"platform"`
		Model    string `json:"model"`
	} `json:"data"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = defaultYoboxBaseURL
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = info.OriginModelName
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	mergeYoboxRequestMetadata(c, &req)
	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	seconds := req.Duration
	if seconds <= 0 {
		if v, ok := parseYoboxSeconds(req.Seconds); ok {
			seconds = v
		}
	}
	if seconds <= 0 {
		seconds = 4
	}
	return map[string]float64{"seconds": float64(seconds)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + yoboxTasksPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	data, err := common.Marshal(body)
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

	var parsed submitResponse
	if err := common.Unmarshal(responseBody, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if !parsed.Success {
		message, redacted := sanitizeYoboxFailureReason(parsed.Message)
		if redacted {
			return "", nil, service.TaskErrorWrapperLocal(errors.New(message), "submit_failed", http.StatusBadRequest)
		}
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("yobox submit failed: %s", message), "submit_failed", http.StatusBadRequest)
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = model.TaskStatus(model.TaskStatusSubmitted).ToVideoStatus()
	c.JSON(http.StatusOK, ov)
	return parsed.Data.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseURL, "/") + yoboxTasksPath + "/" + taskID
	req, err := http.NewRequest(http.MethodGet, uri, nil)
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
	var parsed responseTask
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	info := &relaycommon.TaskInfo{Code: 0}
	info.TaskID = firstNonEmpty(parsed.Data.TaskID, parsed.TaskID, parsed.Data.Data.ID, parsed.Data.ID)
	status := mapYoboxStatus(firstNonEmpty(parsed.Data.Status, parsed.Status, parsed.Data.Data.Status, parsed.Data.Data.Phase, parsed.Data.Phase))
	info.Status = string(status)
	info.Progress = progressString(firstPositive(parsed.Data.Progress, parsed.Progress), status)
	if status == model.TaskStatusSuccess {
		info.Url = firstNonEmpty(parsed.Data.Data.VideoURL, firstString(parsed.Data.Data.Outputs), parsed.Data.Data.URL, parsed.Data.VideoURL, firstString(parsed.Data.Outputs), parsed.Data.URL)
		info.Progress = "100%"
	}
	if status == model.TaskStatusFailure {
		info.Progress = "100%"
		info.Reason, _ = sanitizeYoboxFailureReason(firstNonEmpty(parsed.Data.FailReason, parsed.FailReason, parsed.Data.Data.FailReason, parsed.Data.Data.Error, parsed.Data.Data.Phase, parsed.Data.Error, parsed.Data.Phase, parsed.Message, "task failed"))
	}
	return info, nil
}

func sanitizeYoboxFailureReason(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if strings.Contains(strings.ToLower(message), strings.ToLower(yoboxImageReferencesLimitMessage)) {
		return yoboxGenericProcessingError, true
	}
	return message, false
}

func (a *TaskAdaptor) SanitizeTaskUpstreamError(responseBody []byte) string {
	if message, redacted := sanitizeYoboxFailureReason(string(responseBody)); redacted {
		return message
	}
	return string(responseBody)
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Model = originTask.Properties.OriginModelName
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt
	if url := firstNonEmpty(originTask.GetResultURL(), firstVideoURL(originTask)); url != "" {
		ov.SetMetadata("url", url)
		ov.SetMetadata("video_url", url)
		ov.SetMetadata("result_url", url)
	}
	if originTask.Status == model.TaskStatusFailure {
		ov.Error = &dto.OpenAIVideoError{Message: originTask.FailReason, Code: "failure"}
	}
	return common.Marshal(ov)
}

func (a *TaskAdaptor) GetModelList() []string { return modelList }

func (a *TaskAdaptor) GetChannelName() string { return "yobox" }

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (any, error) {
	modelName := ""
	if info != nil {
		if info.ChannelMeta != nil {
			modelName = strings.TrimSpace(info.UpstreamModelName)
		}
		if modelName == "" {
			modelName = strings.TrimSpace(info.OriginModelName)
		}
	}
	if modelName == "" {
		modelName = strings.TrimSpace(req.Model)
	}
	if modelName == "" {
		modelName = "seedance2"
	}

	if modelName == "seedance2" {
		return convertSeedance2Payload(req, modelName), nil
	}
	return convertSeedance20Payload(req, modelName), nil
}

func convertSeedance2Payload(req *relaycommon.TaskSubmitReq, modelName string) map[string]any {
	payload := map[string]any{
		"model":   modelName,
		"prompt":  req.Prompt,
		"seconds": normalizeYoboxSecondsString(req.Seconds, req.Duration),
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if len(req.Images) == 1 {
		payload["input_reference"] = req.Images[0]
	} else if len(req.Images) > 1 {
		payload["content"] = buildYoboxContent(req.Prompt, req.Images)
	}
	if len(req.Images) > 2 {
		payload["generate_audio"] = false
	}
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "resolution", "generate_audio"} {
			if v, ok := req.Metadata[key]; ok {
				payload[key] = v
			}
		}
	}
	return payload
}

func convertSeedance20Payload(req *relaycommon.TaskSubmitReq, modelName string) map[string]any {
	input := map[string]any{
		"prompt":       req.Prompt,
		"duration":     normalizeYoboxSeconds(req.Seconds, req.Duration),
		"aspect_ratio": firstNonEmpty(stringValue(req.Metadata["aspect_ratio"]), stringValue(req.Metadata["ratio"]), defaultYoboxAspectRatio(req.Size)),
		"resolution":   firstNonEmpty(stringValue(req.Metadata["resolution"]), defaultYoboxResolution(req.Size)),
		"audio":        true,
		"n":            1,
	}
	if len(req.Images) > 0 {
		input["image_references"] = buildYoboxImageReferences(req.Images)
	}
	if v, ok := req.Metadata["audio"]; ok {
		if b, ok := v.(bool); ok {
			input["audio"] = b
		}
	}
	if v, ok := req.Metadata["n"]; ok {
		if n, ok := v.(int); ok && n > 0 {
			input["n"] = n
		}
	}
	return map[string]any{
		"model": modelName,
		"input": input,
	}
}

func buildYoboxImageReferences(images []string) []map[string]any {
	refs := make([]map[string]any, 0, len(images))
	for _, imageURL := range images {
		refs = append(refs, map[string]any{
			"url":      imageURL,
			"strength": "MID",
		})
	}
	return refs
}

func buildYoboxContent(prompt string, images []string) []any {
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for _, imageURL := range images {
		content = append(content, map[string]any{
			"type": "image_url",
			"role": "reference_image",
			"image_url": map[string]any{
				"url": imageURL,
			},
		})
	}
	return content
}

func mapYoboxStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted":
		return model.TaskStatusSubmitted
	case "queued", "pending":
		return model.TaskStatusQueued
	case "in_progress", "running", "processing":
		return model.TaskStatusInProgress
	case "success", "succeeded", "completed", "complete":
		return model.TaskStatusSuccess
	case "failure", "failed", "error":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusInProgress
	}
}

func progressString(progress int, status model.TaskStatus) string {
	if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
		return "100%"
	}
	if progress > 0 {
		return fmt.Sprintf("%d%%", progress)
	}
	if status == model.TaskStatusQueued || status == model.TaskStatusSubmitted {
		return "20%"
	}
	return "30%"
}

func normalizeYoboxSeconds(seconds string, duration int) int {
	if duration > 0 {
		return duration
	}
	if v, ok := parseYoboxSeconds(seconds); ok {
		return v
	}
	return 4
}

func normalizeYoboxSecondsString(seconds string, duration int) string {
	return fmt.Sprintf("%d", normalizeYoboxSeconds(seconds, duration))
}

func parseYoboxSeconds(seconds string) (int, bool) {
	seconds = strings.TrimSpace(strings.ToLower(seconds))
	seconds = strings.TrimSuffix(seconds, "seconds")
	seconds = strings.TrimSuffix(seconds, "second")
	seconds = strings.TrimSuffix(seconds, "secs")
	seconds = strings.TrimSuffix(seconds, "sec")
	seconds = strings.TrimSuffix(seconds, "s")
	seconds = strings.TrimSpace(seconds)
	if seconds == "" {
		return 0, false
	}
	var value int
	if _, err := fmt.Sscanf(seconds, "%d", &value); err == nil && value > 0 {
		return value, true
	}
	return 0, false
}

func mergeYoboxRequestMetadata(c *gin.Context, req *relaycommon.TaskSubmitReq) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		return
	}
	var raw map[string]any
	if err := common.Unmarshal(body, &raw); err != nil {
		return
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	for _, key := range []string{"content", "ratio", "aspect_ratio", "resolution", "generate_audio", "audio", "n"} {
		if v, ok := raw[key]; ok {
			req.Metadata[key] = v
		}
	}
	if input, ok := raw["input"].(map[string]any); ok {
		for _, key := range []string{"image_references", "start_frames", "end_frames", "audio", "n", "aspect_ratio", "resolution"} {
			if v, ok := input[key]; ok {
				req.Metadata[key] = v
			}
		}
	}
	if content, ok := req.Metadata["content"]; ok {
		req.Metadata["content"] = content
	}
	req.Images = mergeYoboxImages(req.Images, extractYoboxContentImages(req.Metadata["content"]))
	if len(req.Images) == 0 {
		req.Images = mergeYoboxImages(req.Images, extractYoboxContentImages(raw["content"]))
	}
}

func extractYoboxContentImages(content any) []string {
	items, ok := content.([]any)
	if !ok {
		return nil
	}
	images := make([]string, 0, len(items))
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		imageURL, ok := itemMap["image_url"].(map[string]any)
		if !ok {
			continue
		}
		url, _ := imageURL["url"].(string)
		if strings.TrimSpace(url) != "" {
			images = append(images, strings.TrimSpace(url))
		}
	}
	return images
}

func mergeYoboxImages(existing, extra []string) []string {
	if len(extra) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return extra
	}
	merged := make([]string, 0, len(existing)+len(extra))
	merged = append(merged, existing...)
	merged = append(merged, extra...)
	return merged
}

func defaultYoboxAspectRatio(size string) string {
	switch strings.TrimSpace(size) {
	case "720x1280":
		return "9:16"
	case "1280x720":
		return "16:9"
	case "720x720":
		return "1:1"
	default:
		return ""
	}
}

func defaultYoboxResolution(size string) string {
	switch strings.TrimSpace(size) {
	case "720x1280", "1280x720", "720x720", "":
		return "720p"
	default:
		return "720p"
	}
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstVideoURL(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var parsed responseTask
	if err := common.Unmarshal(task.Data, &parsed); err == nil {
		if url := firstNonEmpty(parsed.Data.Data.VideoURL, firstString(parsed.Data.Data.Outputs), parsed.Data.Data.URL, parsed.Data.VideoURL, firstString(parsed.Data.Outputs), parsed.Data.URL); url != "" {
			return url
		}
	}
	return ""
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}
