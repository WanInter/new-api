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

	assetPath    = "/v1/sd/assets"
	generatePath = "/async/tasks"
	taskPath     = "/async/tasks"

	defaultAssetPollInterval = time.Second
	defaultAssetPollTimeout  = 90 * time.Second
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
	ID      string    `json:"id"`
	Status  string    `json:"status"`
	Model   string    `json:"model"`
	Outputs []string  `json:"outputs"`
	Usage   taskUsage `json:"usage"`
	Error   any       `json:"error"`
}

type taskUsage struct {
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type asyncSubmitResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

type asyncTaskResponse struct {
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	TaskID     string            `json:"task_id"`
	Status     string            `json:"status"`
	Progress   int               `json:"progress"`
	FailReason string            `json:"fail_reason"`
	Data       asyncTaskEnvelope `json:"data"`
}

type asyncTaskEnvelope struct {
	TaskID     string           `json:"task_id"`
	Status     string           `json:"status"`
	Progress   int              `json:"progress"`
	FailReason string           `json:"fail_reason"`
	Data       asyncTaskPayload `json:"data"`
	Usage      taskUsage        `json:"usage"`

	ID       string   `json:"id"`
	VideoURL string   `json:"video_url"`
	Outputs  []string `json:"outputs"`
	URL      string   `json:"url"`
	Phase    string   `json:"phase"`
	Error    any      `json:"error"`
}

type asyncTaskPayload struct {
	ID         string        `json:"id"`
	Status     string        `json:"status"`
	VideoURL   string        `json:"video_url"`
	Outputs    []string      `json:"outputs"`
	URL        string        `json:"url"`
	Phase      string        `json:"phase"`
	Error      any           `json:"error"`
	FailReason string        `json:"fail_reason"`
	Task       *upstreamTask `json:"task"`
	Usage      taskUsage     `json:"usage"`
}

type assetRequest struct {
	Model     string `json:"model"`
	URL       string `json:"URL"`
	Name      string `json:"Name"`
	AssetType string `json:"AssetType"`
}

type assetResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Error   any       `json:"error"`
	Data    assetData `json:"data"`
}

type assetData struct {
	ID        string `json:"Id"`
	Status    string `json:"Status"`
	AssetType string `json:"AssetType"`
	Error     any    `json:"Error"`
	BaseResp  struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey            string
	baseURL           string
	proxy             string
	assetPollInterval time.Duration
	assetPollTimeout  time.Duration
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
	a.proxy = info.ChannelSetting.Proxy
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

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	modelName := ""
	if info != nil {
		modelName = info.OriginModelName
	}
	multiplier := officialTokenPriceMultiplier(modelName, requestResolution(req), requestHasVideoInput(req))
	if multiplier == 1 {
		return nil
	}
	return map[string]float64{"official_token_price_multiplier": multiplier}
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
	content, _ := payload["content"].([]any)
	content, err = a.uploadReferences(c.Request.Context(), upstreamModelName(req, info), content)
	if err != nil {
		return nil, err
	}
	payload["content"] = content
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

	var nativeResponse generateResponse
	if err := common.Unmarshal(responseBody, &nativeResponse); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamTaskID := ""
	if nativeResponse.Task != nil {
		upstreamTaskID = strings.TrimSpace(nativeResponse.Task.ID)
	}
	if upstreamTaskID == "" {
		var asyncResponse asyncSubmitResponse
		if err := common.Unmarshal(responseBody, &asyncResponse); err != nil {
			return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		}
		upstreamTaskID = strings.TrimSpace(asyncResponse.Data.TaskID)
		if upstreamTaskID == "" && strings.TrimSpace(asyncResponse.Message) != "" {
			return "", nil, service.TaskErrorWrapperLocal(errors.New(asyncResponse.Message), "submit_failed", http.StatusBadGateway)
		}
	}
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("YoboxCorp generate response has no task id"), "invalid_response", http.StatusBadGateway)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	video.Status = model.TaskStatus(model.TaskStatusSubmitted).ToVideoStatus()
	c.JSON(http.StatusOK, video)
	return upstreamTaskID, responseBody, nil
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
	var nativeResponse generateResponse
	if err := common.Unmarshal(respBody, &nativeResponse); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	if nativeResponse.Task != nil {
		return taskInfoFromNativeTask(nativeResponse.Task)
	}

	var asyncResponse asyncTaskResponse
	if err := common.Unmarshal(respBody, &asyncResponse); err != nil {
		return nil, errors.Wrap(err, "unmarshal async task result failed")
	}
	if asyncResponse.Data.Data.Task != nil {
		return taskInfoFromNativeTask(asyncResponse.Data.Data.Task)
	}
	rawStatus := firstNonEmpty(asyncResponse.Data.Status, asyncResponse.Status, asyncResponse.Data.Data.Status, asyncResponse.Data.Data.Phase, asyncResponse.Data.Phase)
	status := mapTaskStatus(rawStatus)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown YoboxCorp task status %q", rawStatus)
	}
	result := &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   firstNonEmpty(asyncResponse.Data.TaskID, asyncResponse.TaskID, asyncResponse.Data.Data.ID, asyncResponse.Data.ID),
		Status:   string(status),
		Progress: progressString(firstPositive(asyncResponse.Data.Progress, asyncResponse.Progress), status),
	}
	if status == model.TaskStatusSuccess {
		result.Url = firstNonEmpty(asyncResponse.Data.Data.VideoURL, firstString(asyncResponse.Data.Data.Outputs), asyncResponse.Data.Data.URL, asyncResponse.Data.VideoURL, firstString(asyncResponse.Data.Outputs), asyncResponse.Data.URL)
		result.Progress = taskcommon.ProgressComplete
		result.CompletionTokens, result.TotalTokens = taskUsageTokens(
			asyncResponse.Data.Data.Usage,
			asyncResponse.Data.Usage,
		)
	}
	if status == model.TaskStatusFailure {
		result.Reason = firstNonEmpty(
			asyncResponse.Data.FailReason,
			asyncResponse.FailReason,
			asyncResponse.Data.Data.FailReason,
			taskErrorMessage(asyncResponse.Data.Data.Error),
			asyncResponse.Data.Data.Phase,
			taskErrorMessage(asyncResponse.Data.Error),
			asyncResponse.Data.Phase,
			asyncResponse.Message,
		)
		if result.Reason == "" {
			result.Reason = "task failed"
		}
		result.Progress = taskcommon.ProgressComplete
	}
	return result, nil
}

func taskInfoFromNativeTask(task *upstreamTask) (*relaycommon.TaskInfo, error) {
	status := mapTaskStatus(task.Status)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown YoboxCorp task status %q", task.Status)
	}
	result := &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   task.ID,
		Status:   string(status),
		Progress: progressForStatus(status),
	}
	if status == model.TaskStatusSuccess {
		result.Url = firstString(task.Outputs)
		result.Progress = taskcommon.ProgressComplete
		result.CompletionTokens, result.TotalTokens = taskUsageTokens(task.Usage)
	}
	if status == model.TaskStatusFailure {
		result.Reason = taskErrorMessage(task.Error)
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

func (a *TaskAdaptor) uploadReferences(ctx context.Context, modelName string, content []any) ([]any, error) {
	assetIDs := make(map[string]string)
	assetCounts := map[string]int{"Image": 0, "Video": 0, "Audio": 0}

	for _, rawItem := range content {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		contentType, _ := item["type"].(string)
		assetType := ""
		switch contentType {
		case "image_url":
			assetType = "Image"
		case "video_url":
			assetType = "Video"
		case "audio_url":
			assetType = "Audio"
		default:
			continue
		}

		reference, ok := item[contentType].(map[string]any)
		if !ok {
			continue
		}
		referenceURL, _ := reference["url"].(string)
		referenceURL = strings.TrimSpace(referenceURL)
		if referenceURL == "" || strings.HasPrefix(strings.ToLower(referenceURL), "asset://") {
			continue
		}

		cacheKey := assetType + "\x00" + referenceURL
		assetID := assetIDs[cacheKey]
		if assetID == "" {
			assetCounts[assetType]++
			assetName := fmt.Sprintf("reference-%s-%03d", strings.ToLower(assetType), assetCounts[assetType])
			var err error
			assetID, err = a.createAndWaitForAsset(ctx, assetRequest{
				Model:     modelName,
				URL:       referenceURL,
				Name:      assetName,
				AssetType: assetType,
			})
			if err != nil {
				return nil, fmt.Errorf("prepare %s asset %q failed: %w", strings.ToLower(assetType), assetName, err)
			}
			assetIDs[cacheKey] = assetID
		}
		reference["url"] = "asset://" + assetID
	}

	return content, nil
}

func (a *TaskAdaptor) createAndWaitForAsset(ctx context.Context, payload assetRequest) (string, error) {
	response, err := a.createAsset(ctx, payload)
	if err != nil {
		return "", err
	}
	assetID := strings.TrimSpace(response.Data.ID)
	if assetID == "" {
		return "", errors.New("asset create response has no Id")
	}
	if status := strings.TrimSpace(response.Data.Status); status != "" {
		ready, err := assetReady(status, response)
		if err != nil {
			return "", err
		}
		if ready {
			return assetID, nil
		}
	}

	pollTimeout := a.assetPollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultAssetPollTimeout
	}
	pollInterval := a.assetPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultAssetPollInterval
	}
	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	for {
		response, err = a.fetchAsset(pollCtx, payload.Model, assetID)
		if err != nil {
			return "", err
		}
		ready, err := assetReady(response.Data.Status, response)
		if err != nil {
			return "", err
		}
		if ready {
			return assetID, nil
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("timed out waiting for asset %s to become Active", assetID)
		case <-timer.C:
		}
	}
}

func (a *TaskAdaptor) createAsset(ctx context.Context, payload assetRequest) (*assetResponse, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode asset create request failed: %w", err)
	}
	return a.doAssetRequest(ctx, http.MethodPost, a.baseURL+assetPath, bytes.NewReader(body))
}

func (a *TaskAdaptor) fetchAsset(ctx context.Context, modelName, assetID string) (*assetResponse, error) {
	uri, err := url.Parse(a.baseURL + assetPath + "/" + url.PathEscape(assetID))
	if err != nil {
		return nil, fmt.Errorf("build asset query URL failed: %w", err)
	}
	query := uri.Query()
	query.Set("model", modelName)
	uri.RawQuery = query.Encode()
	return a.doAssetRequest(ctx, http.MethodGet, uri.String(), nil)
}

func (a *TaskAdaptor) doAssetRequest(ctx context.Context, method, uri string, body io.Reader) (*assetResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, uri, body)
	if err != nil {
		return nil, fmt.Errorf("create asset request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asset request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read asset response failed: %w", err)
	}
	var parsed assetResponse
	if err := common.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode asset response failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("asset request returned status %d: %s", resp.StatusCode, assetResponseMessage(&parsed))
	}
	if !parsed.Success && parsed.Data.ID == "" {
		return nil, fmt.Errorf("asset request failed: %s", assetResponseMessage(&parsed))
	}
	return &parsed, nil
}

func assetReady(status string, response *assetResponse) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return true, nil
	case "processing", "pending", "creating", "uploading":
		return false, nil
	case "failed", "failure", "error", "rejected", "inactive":
		return false, fmt.Errorf("asset processing failed: %s", assetResponseMessage(response))
	case "":
		return false, nil
	default:
		return false, fmt.Errorf("unknown asset status %q", status)
	}
}

func assetResponseMessage(response *assetResponse) string {
	if response == nil {
		return "unknown error"
	}
	message := firstNonEmpty(
		response.Message,
		taskErrorMessage(response.Data.Error),
		taskErrorMessage(response.Error),
		response.Data.BaseResp.StatusMsg,
	)
	if message == "" || strings.EqualFold(message, "success") {
		return "unknown error"
	}
	return message
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

func requestHasVideoInput(req relaycommon.TaskSubmitReq) bool {
	if len(appendURLs(req.Videos, req.VideoURLs)) > 0 {
		return true
	}
	for _, item := range req.Content {
		if strings.EqualFold(strings.TrimSpace(item.Type), "video_url") && item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			return true
		}
	}
	return false
}

// officialTokenPriceMultiplier normalizes every official rate to the video-input
// 480p/720p rate configured as the model's ModelRatio.
func officialTokenPriceMultiplier(modelName, resolution string, hasVideoInput bool) float64 {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	switch modelName {
	case "dreamina-seedance-2-0-hc":
		const baseRate = 4.3
		if hasVideoInput {
			switch resolution {
			case "1080p":
				return 4.7 / baseRate
			case "4k":
				return 2.4 / baseRate
			default:
				return 1
			}
		}
		switch resolution {
		case "1080p":
			return 7.7 / baseRate
		case "4k":
			return 4.0 / baseRate
		default:
			return 7.0 / baseRate
		}
	case "dreamina-seedance-2-0-fast-hc":
		if hasVideoInput {
			return 1
		}
		return 5.6 / 3.3
	case "dreamina-seedance-2-0-mini-hc":
		if hasVideoInput {
			return 1
		}
		return 3.5 / 2.1
	default:
		return 1
	}
}

func taskUsageTokens(usages ...taskUsage) (completionTokens, totalTokens int) {
	for _, usage := range usages {
		if completionTokens == 0 && usage.CompletionTokens > 0 {
			completionTokens = usage.CompletionTokens
		}
		if totalTokens == 0 && usage.TotalTokens > 0 {
			totalTokens = usage.TotalTokens
		}
	}
	if totalTokens == 0 {
		totalTokens = completionTokens
	}
	return completionTokens, totalTokens
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

func progressString(progress int, status model.TaskStatus) string {
	if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
		return taskcommon.ProgressComplete
	}
	if progress > 0 {
		return fmt.Sprintf("%d%%", progress)
	}
	return progressForStatus(status)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
