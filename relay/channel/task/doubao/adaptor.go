package doubao

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
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type mediaURLValue struct {
	URL string
}

func (v *mediaURLValue) UnmarshalJSON(data []byte) error {
	var url string
	if err := common.Unmarshal(data, &url); err == nil {
		v.URL = url
		return nil
	}

	var media *MediaURL
	if err := common.Unmarshal(data, &media); err != nil {
		return err
	}
	if media != nil {
		v.URL = media.URL
	}
	return nil
}

type taskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *taskError) UnmarshalJSON(data []byte) error {
	var message string
	if err := common.Unmarshal(data, &message); err == nil {
		e.Message = message
		return nil
	}
	type alias taskError
	return common.Unmarshal(data, (*alias)(e))
}

type byteforRequestPayload struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Size       string   `json:"size,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	Duration   string   `json:"duration,omitempty"`
	Images     []string `json:"images,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	Resolution  string         `json:"resolution,omitempty"`
	Ratio       string         `json:"ratio,omitempty"`
	Duration    *dto.IntValue  `json:"duration,omitempty"`
	Frames      *dto.IntValue  `json:"frames,omitempty"`
	Seed        *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark   *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID      string `json:"id"`
	TaskID  string `json:"task_id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Created int64  `json:"created"`
}

type responseTask struct {
	ID       string        `json:"id"`
	TaskID   string        `json:"task_id"`
	Model    string        `json:"model"`
	Status   string        `json:"status"`
	Progress int           `json:"progress"`
	VideoURL mediaURLValue `json:"video_url"`
	Data     []struct {
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Content struct {
		VideoURL mediaURLValue `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error        taskError `json:"error"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_msg"`
	ProgressText string    `json:"progress_text"`
	QueueInfo    string    `json:"queue_info"`
	Created      int64     `json:"created"`
	CreatedAt    int64     `json:"created_at"`
	UpdatedAt    int64     `json:"updated_at"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Accept only POST /v1/video/generations as "generate" action.
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := strings.TrimRight(a.baseURL, "/")
	if isByteforModel(relayInfoModelName(info)) {
		baseURL = resolveByteforBaseURL(baseURL)
		return fmt.Sprintf("%s/v1/videos/generations", baseURL), nil
	}
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling returns duration billing for Bytefor and video-input discounts for Doubao.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	if isByteforModel(relayInfoModelName(info)) {
		return map[string]float64{"seconds": float64(byteforBillingSeconds(req))}
	}
	if hasVideoInMetadata(req.Metadata) {
		if ratio, ok := GetVideoInputRatio(info.OriginModelName); ok {
			return map[string]float64{"video_input": ratio}
		}
	}
	return nil
}

// hasVideoInMetadata 直接检查 metadata 的 content 数组是否包含 video_url 条目，
// 避免构建完整的上游 requestPayload。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == "video_url" {
			return true
		}
		if _, has := itemMap["video_url"]; has {
			return true
		}
	}
	return false
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	if isByteforModel(relayInfoModelName(info)) {
		body := convertToByteforRequestPayload(&req, info)
		data, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamTaskID := firstNonEmpty(dResp.ID, dResp.TaskID)
	if upstreamTaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = dResp.Created
	if ov.CreatedAt == 0 {
		ov.CreatedAt = time.Now().Unix()
	}
	ov.Model = info.OriginModelName
	ov.Status = toOpenAIVideoStatus(dResp.Status)

	c.JSON(http.StatusOK, ov)
	return upstreamTaskID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	baseURL := strings.TrimRight(baseUrl, "/")
	modelName, _ := body["model"].(string)
	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseURL, taskID)
	if isByteforModel(modelName) {
		baseURL = resolveByteforBaseURL(baseURL)
		uri = fmt.Sprintf("%s/v1/videos/generations/%s", baseURL, taskID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}

	metadata := req.Metadata
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if req.Duration > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(req.Duration))
	} else if sec := parseSeconds(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	}
	if req.Resolution != "" {
		r.Resolution = req.Resolution
	}
	// Ark calls this field ratio. Keep size as the legacy override, but accept
	// the public video API's aspect_ratio field when size is not supplied.
	r.Ratio = firstNonEmpty(req.Size, req.AspectRatio, r.Ratio)

	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	return &r, nil
}

func convertToByteforRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) *byteforRequestPayload {
	payload := &byteforRequestPayload{
		Model:      relayInfoModelName(info),
		Prompt:     req.Prompt,
		Size:       firstNonEmpty(req.Size, req.AspectRatio, metadataString(req.Metadata, "size", "ratio", "aspect_ratio")),
		Resolution: firstNonEmpty(req.Resolution, metadataString(req.Metadata, "resolution")),
		Duration:   byteforDuration(req),
	}
	if payload.Model == "" {
		payload.Model = req.Model
	}
	payload.Resolution = normalizeByteforResolution(payload.Resolution)

	// TaskSubmitReq keeps several compatibility aliases separately. Preserve
	// every non-empty value and its order because Bytefor binds prompt
	// references to the positional index in images.
	for _, media := range [][]string{
		req.Images,
		req.ImageURLs,
		req.InputStartFrames,
		req.InputImageReferences,
		req.MetadataStartFrames,
		req.Audios,
		req.AudioURLs,
		req.Videos,
		req.VideoURLs,
	} {
		for _, value := range media {
			payload.Images = appendNonEmpty(payload.Images, value)
		}
	}
	for _, item := range req.Content {
		switch item.Type {
		case "image_url":
			if item.ImageURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.ImageURL.URL)
			}
		case "video_url":
			if item.VideoURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.VideoURL.URL)
			}
		case "audio_url":
			if item.AudioURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.AudioURL.URL)
			}
		default:
			// Preserve media even when a compatibility client omits type.
			if item.ImageURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.ImageURL.URL)
			}
			if item.VideoURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.VideoURL.URL)
			}
			if item.AudioURL != nil {
				payload.Images = appendNonEmpty(payload.Images, item.AudioURL.URL)
			}
		}
	}
	for _, value := range []string{req.Image, req.InputReference} {
		payload.Images = appendNonEmpty(payload.Images, value)
	}
	return payload
}

func relayInfoModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return strings.TrimSpace(info.OriginModelName)
}

func resolveByteforBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	defaultDoubaoURL := strings.TrimRight(constant.ChannelBaseURLs[constant.ChannelTypeDoubaoVideo], "/")
	if baseURL == "" || baseURL == defaultDoubaoURL {
		return ByteforBaseURL
	}
	return baseURL
}

func metadataString(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func byteforDuration(req *relaycommon.TaskSubmitReq) string {
	return fmt.Sprintf("%ds", byteforBillingSeconds(*req))
}

func byteforBillingSeconds(req relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if seconds := parseSeconds(req.Seconds); seconds > 0 {
		return seconds
	}
	return byteforDefaultDurationSeconds
}

func parseSeconds(raw string) int {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
		value = strings.TrimSuffix(value, suffix)
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}

func normalizeByteforResolution(resolution string) string {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "720p":
		return "720P"
	case "1080p":
		return "1080P"
	case "4k":
		return "4K"
	default:
		return strings.TrimSpace(resolution)
	}
}

func appendNonEmpty(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	return append(values, value)
}

func extractResponseTaskVideoURL(task responseTask) string {
	if url := firstNonEmpty(task.VideoURL.URL, task.Content.VideoURL.URL); url != "" {
		return url
	}
	for _, item := range task.Data {
		if strings.TrimSpace(item.URL) != "" {
			return strings.TrimSpace(item.URL)
		}
	}
	return ""
}

func extractResponseTaskError(task responseTask) string {
	return firstNonEmpty(
		task.Error.Message,
		task.ErrorMessage,
		task.ProgressText,
		task.QueueInfo,
		task.ErrorCode,
	)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao and Bytefor statuses to internal status.
	switch strings.ToLower(strings.TrimSpace(resTask.Status)) {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running", "in_progress", "configuring", "generating":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded", "completed":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = extractResponseTaskVideoURL(resTask)
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = extractResponseTaskError(resTask)
	default:
		return nil, fmt.Errorf("unknown Doubao task status %q", resTask.Status)
	}
	if resTask.Progress > 0 && resTask.Progress <= 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	if url := extractResponseTaskVideoURL(dResp); url != "" {
		openAIVideo.SetMetadata("url", url)
	}
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.CompletionTime()
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: extractResponseTaskError(dResp),
			Code:    firstNonEmpty(dResp.Error.Code, dResp.ErrorCode),
		}
	}

	return common.Marshal(openAIVideo)
}

func toOpenAIVideoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "processing", "running", "in_progress", "configuring", "generating":
		return dto.VideoStatusInProgress
	case "succeeded", "completed":
		return dto.VideoStatusCompleted
	case "failed":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusQueued
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
