package yobox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	defaultYoboxBaseURL = "https://max.yoboxai.com"
	yoboxTasksPath      = "/async/tasks"
)

var modelList = []string{
	"seedance2",
	"seedance2-pro",
	"seedance-2.0",
	"seedance-2.0-fast",
	"seedance-2.0-noface",
	"seedance-2.0-fast-noface",
	"happy-horse-1.1",
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
	Error    any      `json:"error"`
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
	Error      any      `json:"error"`
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
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	if err := validateYoboxSeedance20Size(&req, info); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return nil
}

// ValidateMappedRequest runs after channel model mapping. The legacy Seedance2
// wire format accepts image references only, so video/audio inputs must fail
// before pricing instead of being silently omitted from the outgoing payload.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if err := validateYoboxSeedance2Media(&req, info); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if err := validateYoboxSeedance20Size(&req, info); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
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
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("yobox submit failed: %s", strings.TrimSpace(parsed.Message)), "submit_failed", http.StatusBadRequest)
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

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseURL, "/") + yoboxTasksPath + "/" + taskID
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
	var parsed responseTask
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	info := &relaycommon.TaskInfo{Code: 0}
	info.TaskID = firstNonEmpty(parsed.Data.TaskID, parsed.TaskID, parsed.Data.Data.ID, parsed.Data.ID)
	rawStatus := firstNonEmpty(parsed.Data.Status, parsed.Status, parsed.Data.Data.Status, parsed.Data.Data.Phase, parsed.Data.Phase)
	status := mapYoboxStatus(rawStatus)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown Yobox task status %q", rawStatus)
	}
	info.Status = string(status)
	info.Progress = progressString(firstPositive(parsed.Data.Progress, parsed.Progress), status)
	if status == model.TaskStatusSuccess {
		info.Url = firstNonEmpty(parsed.Data.Data.VideoURL, firstString(parsed.Data.Data.Outputs), parsed.Data.Data.URL, parsed.Data.VideoURL, firstString(parsed.Data.Outputs), parsed.Data.URL)
		info.Progress = "100%"
	}
	if status == model.TaskStatusFailure {
		info.Progress = "100%"
		info.Reason = firstNonEmpty(
			parsed.Data.FailReason,
			parsed.FailReason,
			parsed.Data.Data.FailReason,
			yoboxTaskErrorMessage(parsed.Data.Data.Error),
			parsed.Data.Data.Phase,
			yoboxTaskErrorMessage(parsed.Data.Error),
			parsed.Data.Phase,
			parsed.Message,
			"task failed",
		)
	}
	return info, nil
}

func yoboxTaskErrorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"message", "error", "detail", "reason", "code"} {
			if message := yoboxTaskErrorMessage(typed[key]); message != "" {
				return message
			}
		}
	}
	return ""
}

func (a *TaskAdaptor) SanitizeTaskUpstreamError(responseBody []byte) string {
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
	ov.CompletedAt = originTask.CompletionTime()
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
	modelName := yoboxRequestModelName(req, info)
	if modelName == "" {
		modelName = "seedance2"
	}

	if isYoboxSeedance2Model(modelName) {
		return convertSeedance2Payload(req, modelName), nil
	}
	return convertSeedance20Payload(req, modelName), nil
}

func convertSeedance2Payload(req *relaycommon.TaskSubmitReq, modelName string) map[string]any {
	payload := copyYoboxMetadata(req.Metadata, "model", "prompt", "input", "duration", "seconds", "aspect_ratio")
	payload["model"] = modelName
	payload["prompt"] = req.Prompt
	if seconds, ok := yoboxSeedance2Seconds(req); ok {
		payload["seconds"] = seconds
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.Resolution != "" {
		payload["resolution"] = req.Resolution
	}
	if _, ok := payload["ratio"]; !ok {
		if ratio := firstNonEmpty(req.AspectRatio, stringValue(req.Metadata["aspect_ratio"])); ratio != "" {
			payload["ratio"] = ratio
		}
	}
	if _, hasContent := payload["content"]; !hasContent {
		images := yoboxRequestImages(req, true)
		if len(images) == 1 {
			payload["input_reference"] = images[0]
		} else if len(images) > 1 {
			payload["content"] = buildYoboxContent(req.Prompt, images)
		}
	}
	return payload
}

func convertSeedance20Payload(req *relaycommon.TaskSubmitReq, modelName string) map[string]any {
	input := copyYoboxMetadata(req.Metadata, "model", "prompt", "input", "seconds", "size")
	input["prompt"] = req.Prompt
	setYoboxDuration(input, req)
	setYoboxAspectRatio(input, req)
	setYoboxResolution(input, req)

	images := yoboxRequestImages(req, false)
	_, contentVideos, contentAudios := yoboxContentMediaURLs(req.Content)
	if _, hasInputReferences := input["image_references"]; !hasInputReferences {
		images = append(images, req.InputImageReferences...)
	}
	if references := buildYoboxImageReferences(images); len(references) > 0 {
		input["image_references"] = mergeYoboxReferences(input["image_references"], references)
	}
	if references := buildYoboxMediaReferences(req.Videos, req.VideoURLs, contentVideos); len(references) > 0 {
		input["video_references"] = mergeYoboxReferences(input["video_references"], references)
	}
	if references := buildYoboxMediaReferences(req.Audios, req.AudioURLs, contentAudios); len(references) > 0 {
		input["audio_references"] = mergeYoboxReferences(input["audio_references"], references)
	}
	if _, hasStartFrames := input["start_frames"]; !hasStartFrames && len(req.InputStartFrames) > 0 {
		input["start_frames"] = req.InputStartFrames
	}
	if _, hasStartFrames := input["start_frames"]; !hasStartFrames && len(req.MetadataStartFrames) > 0 {
		input["start_frames"] = req.MetadataStartFrames
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
			"url": imageURL,
		})
	}
	return refs
}

func buildYoboxMediaReferences(groups ...[]string) []map[string]any {
	refs := make([]map[string]any, 0)
	for _, group := range groups {
		for _, mediaURL := range group {
			refs = append(refs, map[string]any{"url": mediaURL})
		}
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

func isYoboxSeedance2Model(modelName string) bool {
	switch strings.TrimSpace(modelName) {
	case "seedance2", "seedance2-pro":
		return true
	default:
		return false
	}
}

func validateYoboxSeedance2Media(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) error {
	if req == nil || !isYoboxSeedance2Model(yoboxRequestModelName(req, info)) {
		return nil
	}
	if len(req.Videos) > 0 || len(req.VideoURLs) > 0 || len(req.Audios) > 0 || len(req.AudioURLs) > 0 {
		return fmt.Errorf("Yobox Seedance2 does not support video or audio reference inputs")
	}
	for _, item := range req.Content {
		if item.VideoURL != nil || item.AudioURL != nil {
			return fmt.Errorf("Yobox Seedance2 does not support video or audio reference inputs")
		}
	}
	return nil
}

func isYoboxSeedance20Model(modelName string) bool {
	switch strings.TrimSpace(modelName) {
	case "seedance-2.0", "seedance-2.0-fast", "seedance-2.0-noface", "seedance-2.0-fast-noface":
		return true
	default:
		return false
	}
}

func yoboxRequestModelName(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil {
		if info.ChannelMeta != nil {
			if modelName := strings.TrimSpace(info.UpstreamModelName); modelName != "" {
				return modelName
			}
		}
		if modelName := strings.TrimSpace(info.OriginModelName); modelName != "" {
			return modelName
		}
	}
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.Model)
}

// Seedance 2.0 accepts aspect ratio and resolution as independent fields.
// Its legacy size compatibility is limited to the aliases below, so an
// unknown value must not be silently discarded or reinterpreted.
func validateYoboxSeedance20Size(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) error {
	if req == nil || !isYoboxSeedance20Model(yoboxRequestModelName(req, info)) {
		return nil
	}

	size := yoboxRequestSize(req)
	if size == "" || (yoboxAspectRatioFromSize(size) != "" && yoboxResolutionFromSize(size) != "") {
		return nil
	}
	return fmt.Errorf("size %q is not supported for Yobox Seedance 2.0; use aspect_ratio and resolution without size", size)
}

func yoboxRequestSize(req *relaycommon.TaskSubmitReq) string {
	if req == nil {
		return ""
	}
	return firstNonEmpty(req.Size, stringValue(req.Metadata["size"]))
}

func copyYoboxMetadata(metadata map[string]any, excludedKeys ...string) map[string]any {
	excluded := make(map[string]struct{}, len(excludedKeys))
	for _, key := range excludedKeys {
		excluded[key] = struct{}{}
	}
	values := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if _, skip := excluded[key]; !skip {
			values[key] = value
		}
	}
	return values
}

func yoboxSeedance2Seconds(req *relaycommon.TaskSubmitReq) (string, bool) {
	if duration, ok := req.Metadata["duration"]; ok {
		return yoboxStringValue(duration), true
	}
	if req.Duration != 0 {
		return fmt.Sprintf("%d", req.Duration), true
	}
	if strings.TrimSpace(req.Seconds) != "" {
		if seconds, ok := parseYoboxSeconds(req.Seconds); ok {
			return fmt.Sprintf("%d", seconds), true
		}
		return req.Seconds, true
	}
	if seconds, ok := req.Metadata["seconds"]; ok {
		return yoboxStringValue(seconds), true
	}
	return "", false
}

func yoboxStringValue(value any) string {
	if value == nil {
		return ""
	}
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	return fmt.Sprint(value)
}

func setYoboxDuration(input map[string]any, req *relaycommon.TaskSubmitReq) {
	if _, hasDuration := input["duration"]; hasDuration {
		return
	}
	if req.Duration != 0 {
		input["duration"] = req.Duration
		return
	}

	seconds := req.Seconds
	if strings.TrimSpace(seconds) == "" {
		if value, ok := req.Metadata["seconds"]; ok {
			switch typed := value.(type) {
			case string:
				seconds = typed
			default:
				input["duration"] = typed
				return
			}
		}
	}
	if strings.TrimSpace(seconds) == "" {
		return
	}
	if duration, ok := parseYoboxSeconds(seconds); ok {
		input["duration"] = duration
		return
	}
	// Preserve an invalid alias for the upstream to reject instead of inventing a default.
	input["duration"] = seconds
}

func setYoboxAspectRatio(input map[string]any, req *relaycommon.TaskSubmitReq) {
	if req.AspectRatio != "" {
		input["aspect_ratio"] = req.AspectRatio
		delete(input, "ratio")
		delete(input, "aspectRatio")
		return
	}
	if _, hasAspectRatio := input["aspect_ratio"]; hasAspectRatio {
		delete(input, "ratio")
		delete(input, "aspectRatio")
		return
	}
	if ratio, ok := input["ratio"]; ok {
		input["aspect_ratio"] = ratio
		delete(input, "ratio")
		delete(input, "aspectRatio")
		return
	}
	if aspectRatio, ok := input["aspectRatio"]; ok {
		input["aspect_ratio"] = aspectRatio
		delete(input, "aspectRatio")
		return
	}
	if aspectRatio := yoboxAspectRatioFromSize(yoboxRequestSize(req)); aspectRatio != "" {
		input["aspect_ratio"] = aspectRatio
	}
}

func setYoboxResolution(input map[string]any, req *relaycommon.TaskSubmitReq) {
	if req.Resolution != "" {
		input["resolution"] = req.Resolution
		return
	}
	if _, hasResolution := input["resolution"]; hasResolution {
		return
	}
	if resolution := yoboxResolutionFromSize(yoboxRequestSize(req)); resolution != "" {
		input["resolution"] = resolution
	}
}

func yoboxRequestImages(req *relaycommon.TaskSubmitReq, includeInputReferences bool) []string {
	images := make([]string, 0, len(req.Images)+len(req.ImageURLs)+len(req.InputImageReferences)+2)
	images = append(images, req.Images...)
	images = append(images, req.ImageURLs...)
	if req.Image != "" {
		images = append(images, req.Image)
	}
	if req.InputReference != "" {
		images = append(images, req.InputReference)
	}
	if includeInputReferences {
		images = append(images, req.InputImageReferences...)
	}
	contentImages, _, _ := yoboxContentMediaURLs(req.Content)
	images = append(images, contentImages...)
	return images
}

func yoboxContentMediaURLs(content []relaycommon.TaskContentItem) (images, videos, audios []string) {
	for _, item := range content {
		if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
			images = append(images, strings.TrimSpace(item.ImageURL.URL))
		}
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			videos = append(videos, strings.TrimSpace(item.VideoURL.URL))
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			audios = append(audios, strings.TrimSpace(item.AudioURL.URL))
		}
	}
	return images, videos, audios
}

func mergeYoboxReferences(existing any, additions []map[string]any) any {
	if len(additions) == 0 {
		return existing
	}
	if existing == nil {
		return additions
	}

	merged := make([]any, 0, len(additions)+1)
	switch references := existing.(type) {
	case []any:
		merged = append(merged, references...)
	case []map[string]any:
		for _, reference := range references {
			merged = append(merged, reference)
		}
	case []string:
		for _, reference := range references {
			merged = append(merged, reference)
		}
	default:
		merged = append(merged, existing)
	}
	for _, reference := range additions {
		merged = append(merged, reference)
	}
	return merged
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
		return model.TaskStatusUnknown
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
	value, err := strconv.Atoi(seconds)
	if err == nil && value > 0 {
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
	for key, value := range raw {
		if yoboxTopLevelMetadataField(key, value) {
			req.Metadata[key] = value
		}
	}
	if input, ok := raw["input"].(map[string]any); ok {
		for key, value := range input {
			req.Metadata[key] = value
		}
	}
}

func yoboxTopLevelMetadataField(key string, value any) bool {
	switch key {
	case "model", "prompt", "mode", "image", "images", "image_urls", "video", "videos", "video_url", "video_urls", "audios", "audio_url", "audio_urls", "input_reference", "size", "metadata", "input":
		return false
	case "audio":
		_, isAudioSwitch := value.(bool)
		return isAudioSwitch
	default:
		return true
	}
}

func yoboxAspectRatioFromSize(size string) string {
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

func yoboxResolutionFromSize(size string) string {
	switch strings.TrimSpace(size) {
	case "720x1280", "1280x720", "720x720":
		return "720p"
	default:
		return ""
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
