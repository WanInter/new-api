package hailuo

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
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// https://platform.minimaxi.com/docs/api-reference/video-generation-intro
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

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// ValidateMappedRequest runs after channel model mapping has selected the
// actual Hailuo model. Resolution and image-input support are model-specific,
// so validating them here keeps invalid requests from reaching billing or the
// upstream API.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if _, err := resolveHailuoRequestResolution(&req, hailuoModelConfig(&req, info)); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	if hasHailuoVideoOrAudioInput(&req) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("Hailuo does not support video or audio reference inputs"), "invalid_request", http.StatusBadRequest)
	}
	if _, _, err := resolveHailuoImageInput(&req, hailuoModelName(&req, info)); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", a.baseURL, TextToVideoEndpoint), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
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

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var hResp VideoResponse
	if err := common.Unmarshal(responseBody, &hResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if hResp.BaseResp.StatusCode != StatusSuccess {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("hailuo api error: %s", hResp.BaseResp.StatusMsg),
			strconv.Itoa(hResp.BaseResp.StatusCode),
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return hResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s%s?task_id=%s", baseUrl, QueryTaskEndpoint, taskID)

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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*VideoRequest, error) {
	modelConfig := hailuoModelConfig(req, info)
	duration := DefaultDuration
	if req.Duration > 0 {
		duration = req.Duration
	}
	resolution, err := resolveHailuoRequestResolution(req, modelConfig)
	if err != nil {
		return nil, err
	}

	videoRequest := &VideoRequest{
		Model:      hailuoModelName(req, info),
		Prompt:     req.Prompt,
		Duration:   &duration,
		Resolution: resolution,
	}
	if err := req.UnmarshalMetadata(&videoRequest); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata to video request failed")
	}
	// Metadata is a compatibility input only. The resolved public field must
	// win so metadata cannot reintroduce an unsupported upstream label.
	videoRequest.Resolution = resolution
	firstFrame, subjectReference, err := resolveHailuoImageInput(req, videoRequest.Model)
	if err != nil {
		return nil, err
	}
	if firstFrame != "" {
		videoRequest.FirstFrameImage = firstFrame
	}
	if len(subjectReference) > 0 {
		videoRequest.SubjectReference = subjectReference
	}
	if videoRequest.Duration == nil || !containsInt(modelConfig.SupportedDurations, *videoRequest.Duration) {
		return nil, fmt.Errorf("duration is not supported by Hailuo model %q; supported durations: %v", modelConfig.Name, modelConfig.SupportedDurations)
	}

	return videoRequest, nil
}

func hailuoModelConfig(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) ModelConfig {
	return GetModelConfig(hailuoModelName(req, info))
}

func hailuoModelName(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil && info.ChannelMeta != nil {
		if modelName := strings.TrimSpace(info.UpstreamModelName); modelName != "" {
			return modelName
		}
	}
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.Model)
}

// resolveHailuoImageInput maps the unified images field to the one image
// shape accepted by each Hailuo image-capable model. T2V models have no image
// input, while S2V-01 accepts exactly one character reference.
func resolveHailuoImageInput(req *relaycommon.TaskSubmitReq, modelName string) (string, []SubjectReference, error) {
	if hasHailuoVideoOrAudioInput(req) {
		return "", nil, fmt.Errorf("Hailuo does not support video or audio reference inputs")
	}
	images := hailuoImageInputs(req)
	if len(images) == 0 {
		return "", nil, nil
	}
	modelName = strings.TrimSpace(modelName)
	switch modelName {
	case "MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02", "I2V-01-Director", "I2V-01-live", "I2V-01":
		if len(images) != 1 {
			return "", nil, fmt.Errorf("Hailuo model %q supports exactly one first-frame image", modelName)
		}
		return images[0], nil, nil
	case "S2V-01":
		if len(images) != 1 {
			return "", nil, fmt.Errorf("Hailuo model %q supports exactly one subject reference image", modelName)
		}
		return "", []SubjectReference{{Type: "character", Image: []string{images[0]}}}, nil
	default:
		return "", nil, fmt.Errorf("Hailuo model %q does not support image inputs", modelName)
	}
}

// Canonical images take precedence. Keep image_urls and image as compatibility
// fallbacks for established clients.
func hailuoImageInputs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	images := []string(nil)
	for _, candidates := range [][]string{req.Images, req.ImageURLs} {
		if images = nonEmptyHailuoImages(candidates); len(images) > 0 {
			break
		}
	}
	if len(images) == 0 {
		if image := strings.TrimSpace(req.Image); image != "" {
			images = []string{image}
		}
	}
	if len(images) == 0 {
		if image := strings.TrimSpace(req.InputReference); image != "" {
			images = []string{image}
		}
	}
	if len(images) == 0 {
		for _, candidates := range [][]string{req.InputStartFrames, req.InputImageReferences, req.MetadataStartFrames} {
			if images = nonEmptyHailuoImages(candidates); len(images) > 0 {
				break
			}
		}
	}
	for _, item := range req.Content {
		if item.ImageURL != nil {
			images = appendHailuoImage(images, item.ImageURL.URL)
		}
	}
	return images
}

func nonEmptyHailuoImages(images []string) []string {
	values := make([]string, 0, len(images))
	for _, image := range images {
		if image = strings.TrimSpace(image); image != "" {
			values = append(values, image)
		}
	}
	return values
}

func appendHailuoImage(images []string, image string) []string {
	image = strings.TrimSpace(image)
	if image == "" {
		return images
	}
	return append(images, image)
}

func hasHailuoVideoOrAudioInput(req *relaycommon.TaskSubmitReq) bool {
	if req == nil {
		return false
	}
	for _, values := range [][]string{req.Videos, req.VideoURLs, req.Audios, req.AudioURLs} {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	for _, item := range req.Content {
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			return true
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			return true
		}
	}
	return false
}

// Hailuo itself accepts a quality label, not an arbitrary output geometry.
// These exact pixel forms are retained only as legacy aliases previously
// accepted by this adaptor. Do not infer a label from an arbitrary WxH value.
var hailuoLegacySizeResolutions = map[string]string{
	"1280x720":  Resolution720P,
	"720x1280":  Resolution720P,
	"1920x1080": Resolution1080P,
	"1080x1920": Resolution1080P,
}

func resolveHailuoRequestResolution(req *relaycommon.TaskSubmitReq, modelConfig ModelConfig) (string, error) {
	if req == nil {
		return "", fmt.Errorf("video request is required")
	}

	if value := strings.TrimSpace(req.Resolution); value != "" {
		return validateHailuoResolution(value, modelConfig, "resolution")
	}
	if value, ok, err := hailuoMetadataString(req.Metadata, "resolution"); err != nil {
		return "", err
	} else if ok && strings.TrimSpace(value) != "" {
		return validateHailuoResolution(value, modelConfig, "metadata.resolution")
	}
	if value := strings.TrimSpace(req.Size); value != "" {
		return parseHailuoLegacySize(value, modelConfig)
	}

	return modelConfig.DefaultResolution, nil
}

func hailuoMetadataString(metadata map[string]interface{}, key string) (string, bool, error) {
	if metadata == nil {
		return "", false, nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return "", false, nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("metadata.%s must be a string", key)
	}
	return stringValue, true, nil
}

func parseHailuoLegacySize(size string, modelConfig ModelConfig) (string, error) {
	// A historical client may have used size: "720p". It is still a quality
	// label, so handle it explicitly before considering a pixel size alias.
	if resolution, err := hailuoResolutionLabel(size); err == nil {
		return ensureHailuoResolutionSupported(resolution, modelConfig, "size")
	}

	legacyPixelSize := strings.NewReplacer("*", "x", "×", "x").Replace(size)
	canonical, _, _, pixelSize, err := relaycommon.NormalizeVideoPixelSize(legacyPixelSize)
	if err != nil {
		return "", fmt.Errorf("size %q is not a supported Hailuo legacy size: %w", size, err)
	}
	if !pixelSize {
		return "", fmt.Errorf("size %q is not supported by Hailuo; use resolution instead", size)
	}
	resolution, ok := hailuoLegacySizeResolutions[canonical]
	if !ok {
		return "", fmt.Errorf("size %q is not supported by Hailuo; use resolution instead", size)
	}
	return ensureHailuoResolutionSupported(resolution, modelConfig, "size")
}

func validateHailuoResolution(value string, modelConfig ModelConfig, field string) (string, error) {
	resolution, err := hailuoResolutionLabel(value)
	if err != nil {
		return "", fmt.Errorf("%s %q is not supported by Hailuo; use 512p, 720p, 768p, or 1080p", field, value)
	}
	return ensureHailuoResolutionSupported(resolution, modelConfig, field)
}

func hailuoResolutionLabel(value string) (string, error) {
	resolution, err := relaycommon.NormalizeVideoOutputResolution(value)
	if err != nil {
		return "", err
	}
	switch resolution {
	case "512p":
		return Resolution512P, nil
	case "720p":
		return Resolution720P, nil
	case "768p":
		return Resolution768P, nil
	case "1080p":
		return Resolution1080P, nil
	default:
		return "", fmt.Errorf("unsupported Hailuo resolution")
	}
}

func ensureHailuoResolutionSupported(resolution string, modelConfig ModelConfig, field string) (string, error) {
	for _, supported := range modelConfig.SupportedResolutions {
		if resolution == supported {
			return resolution, nil
		}
	}
	return "", fmt.Errorf("%s %q is not supported by Hailuo model %q; supported resolutions: %s", field, strings.ToLower(resolution), modelConfig.Name, strings.Join(modelConfig.SupportedResolutions, ", "))
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	return a.ParseTaskResultWithContext(context.Background(), respBody)
}

func (a *TaskAdaptor) ParseTaskResultWithContext(ctx context.Context, respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := QueryTaskResponse{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{}

	if resTask.BaseResp.StatusCode == StatusSuccess {
		taskResult.Code = 0
	} else {
		taskResult.Code = resTask.BaseResp.StatusCode
		taskResult.Reason = resTask.BaseResp.StatusMsg
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		return &taskResult, nil
	}

	switch resTask.Status {
	case TaskStatusPreparing, TaskStatusQueueing, TaskStatusProcessing:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
		if resTask.Status == TaskStatusProcessing {
			taskResult.Progress = "50%"
		}
	case TaskStatusSuccess:
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = a.buildVideoURL(ctx, resTask.TaskID, resTask.FileID)
	case TaskStatusFailed:
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
	default:
		return nil, fmt.Errorf("unknown Hailuo task status %q", resTask.Status)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var hailuoResp QueryTaskResponse
	if err := common.Unmarshal(originTask.Data, &hailuoResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal hailuo task data failed")
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if hailuoResp.BaseResp.StatusCode != StatusSuccess {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: hailuoResp.BaseResp.StatusMsg,
			Code:    strconv.Itoa(hailuoResp.BaseResp.StatusCode),
		}
	}

	jsonData, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, errors.Wrap(err, "marshal openai video failed")
	}

	return jsonData, nil
}

func (a *TaskAdaptor) buildVideoURL(ctx context.Context, _, fileID string) string {
	if a.apiKey == "" || a.baseURL == "" {
		return ""
	}

	url := fmt.Sprintf("%s/v1/files/retrieve?file_id=%s", a.baseURL, fileID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var retrieveResp RetrieveFileResponse
	if err := common.Unmarshal(responseBody, &retrieveResp); err != nil {
		return ""
	}

	if retrieveResp.BaseResp.StatusCode != StatusSuccess {
		return ""
	}

	return retrieveResp.File.DownloadURL
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
