package vidu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

type requestPayload struct {
	Model             string   `json:"model"`
	Images            []string `json:"images"`
	Prompt            string   `json:"prompt,omitempty"`
	Duration          int      `json:"duration,omitempty"`
	Seed              int      `json:"seed,omitempty"`
	Resolution        string   `json:"resolution,omitempty"`
	MovementAmplitude string   `json:"movement_amplitude,omitempty"`
	Bgm               bool     `json:"bgm,omitempty"`
	Payload           string   `json:"payload,omitempty"`
	CallbackUrl       string   `json:"callback_url,omitempty"`
}

type responsePayload struct {
	TaskId            string   `json:"task_id"`
	State             string   `json:"state"`
	Model             string   `json:"model"`
	Images            []string `json:"images"`
	Prompt            string   `json:"prompt"`
	Duration          int      `json:"duration"`
	Seed              int      `json:"seed"`
	Resolution        string   `json:"resolution"`
	Bgm               bool     `json:"bgm"`
	MovementAmplitude string   `json:"movement_amplitude"`
	Payload           string   `json:"payload"`
	CreatedAt         string   `json:"created_at"`
}

type taskResultResponse struct {
	State     string     `json:"state"`
	ErrCode   string     `json:"err_code"`
	Credits   int        `json:"credits"`
	Payload   string     `json:"payload"`
	Creations []creation `json:"creations"`
}

type creation struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	CoverURL string `json:"cover_url"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	baseURL     string
}

// Vidu's resolution field is a quality label. Retain only the concrete pixel
// forms that older clients used as compatibility aliases; arbitrary WxH values
// must not be forwarded as a resolution value.
var viduLegacySizeResolutions = map[string]string{
	"1280x720":  "720p",
	"720x1280":  "720p",
	"1920x1080": "1080p",
	"1080x1920": "1080p",
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	action := constant.TaskActionTextGenerate
	if meatAction, ok := req.Metadata["action"]; ok {
		action, _ = meatAction.(string)
	} else if images := viduImageInputs(&req); len(images) > 0 {
		action = constant.TaskActionGenerate
		if info.ChannelType == constant.ChannelTypeVidu {
			// vidu 增加 首尾帧生视频和参考图生视频
			if hasViduImageReferenceInput(&req) {
				action = constant.TaskActionReferenceGenerate
			} else if len(images) == 2 {
				action = constant.TaskActionFirstTailGenerate
			} else if len(images) > 2 {
				action = constant.TaskActionReferenceGenerate
			}
		}
	}
	info.Action = action
	return nil
}

// ValidateMappedRequest runs after model mapping and before billing. Vidu only
// accepts image references, so unsupported media cannot be silently omitted.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, _ *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if _, err := resolveViduRequestResolution(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	if err := validateViduMediaInputs(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, err
	}

	if info.Action == constant.TaskActionReferenceGenerate {
		if strings.Contains(body.Model, "viduq2") {
			// 参考图生视频只能用 viduq2 模型, 不能带有pro或turbo后缀 https://platform.vidu.cn/docs/reference-to-video
			body.Model = "viduq2"
		}
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch info.Action {
	case constant.TaskActionGenerate:
		path = "/img2video"
	case constant.TaskActionFirstTailGenerate:
		path = "/start-end2video"
	case constant.TaskActionReferenceGenerate:
		path = "/reference2video"
	default:
		path = "/text2video"
	}
	return fmt.Sprintf("%s/ent/v2%s", a.baseURL, path), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+info.ApiKey)
	return nil
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

	var vResp responsePayload
	err = common.Unmarshal(responseBody, &vResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrap(err, fmt.Sprintf("%s", responseBody)), "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}

	if vResp.State == "failed" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("task failed"), "task_failed", http.StatusBadRequest)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return vResp.TaskId, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	url := fmt.Sprintf("%s/ent/v2/tasks/%s/creations", baseUrl, taskID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"viduq2", "viduq1", "vidu2.0", "vidu1.5"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "vidu"
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	resolution, err := resolveViduRequestResolution(req)
	if err != nil {
		return nil, err
	}
	if err := validateViduMediaInputs(req); err != nil {
		return nil, err
	}
	images := viduImageInputs(req)
	r := requestPayload{
		Model:             taskcommon.DefaultString(info.UpstreamModelName, "viduq1"),
		Images:            images,
		Prompt:            req.Prompt,
		Duration:          taskcommon.DefaultInt(req.Duration, 5),
		Resolution:        taskcommon.DefaultString(resolution, "1080p"),
		MovementAmplitude: "auto",
		Bgm:               false,
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	if resolution != "" {
		r.Resolution = resolution
	}
	// Public media fields take precedence over provider-specific metadata.
	r.Images = images
	return &r, nil
}

func validateViduMediaInputs(req *relaycommon.TaskSubmitReq) error {
	if hasViduVideoOrAudioInput(req) {
		return fmt.Errorf("Vidu does not support video or audio reference inputs")
	}
	return nil
}

// Normalize every supported public image spelling into Vidu's images field.
// Repeated URLs are de-duplicated because ValidateBasicTaskRequest expands an
// image alias into images for compatibility.
func viduImageInputs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	images := []string(nil)
	for _, candidates := range [][]string{
		req.Images,
		{req.Image},
		req.ImageURLs,
		{req.InputReference},
		req.InputStartFrames,
		req.InputImageReferences,
		req.MetadataStartFrames,
	} {
		for _, image := range candidates {
			images = appendViduImage(images, image)
		}
	}
	for _, item := range req.Content {
		if item.ImageURL != nil {
			images = appendViduImage(images, item.ImageURL.URL)
		}
	}
	return images
}

func appendViduImage(images []string, image string) []string {
	image = strings.TrimSpace(image)
	if image == "" {
		return images
	}
	for _, existing := range images {
		if existing == image {
			return images
		}
	}
	return append(images, image)
}

func hasViduImageReferenceInput(req *relaycommon.TaskSubmitReq) bool {
	if req == nil {
		return false
	}
	for _, image := range req.InputImageReferences {
		if strings.TrimSpace(image) != "" {
			return true
		}
	}
	return false
}

func hasViduVideoOrAudioInput(req *relaycommon.TaskSubmitReq) bool {
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

func resolveViduRequestResolution(req *relaycommon.TaskSubmitReq) (string, error) {
	if req == nil {
		return "", fmt.Errorf("video request is required")
	}

	if value := strings.TrimSpace(req.Resolution); value != "" {
		return viduResolutionLabel(value, "resolution")
	}
	if value, ok, err := viduMetadataString(req.Metadata, "resolution"); err != nil {
		return "", err
	} else if ok && strings.TrimSpace(value) != "" {
		return viduResolutionLabel(value, "metadata.resolution")
	}
	if value := strings.TrimSpace(req.Size); value != "" {
		return parseViduLegacySize(value)
	}
	if req.VideoOutput == nil {
		if value, ok, err := viduMetadataString(req.Metadata, "size"); err != nil {
			return "", err
		} else if ok && strings.TrimSpace(value) != "" {
			return parseViduLegacySize(value)
		}
	}
	return "", nil
}

func viduMetadataString(metadata map[string]any, key string) (string, bool, error) {
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

func viduResolutionLabel(value, field string) (string, error) {
	resolution, err := relaycommon.NormalizeVideoOutputResolution(value)
	if err != nil {
		return "", fmt.Errorf("%s %q is not supported by Vidu; use a quality label such as 720p", field, value)
	}
	return resolution, nil
}

func parseViduLegacySize(size string) (string, error) {
	if resolution, err := viduResolutionLabel(size, "size"); err == nil {
		return resolution, nil
	}

	canonical, _, _, pixelSize, err := relaycommon.NormalizeVideoPixelSize(size)
	if err != nil {
		return "", fmt.Errorf("size %q is not a supported Vidu legacy size: %w", size, err)
	}
	if !pixelSize {
		return "", fmt.Errorf("size %q is not supported by Vidu; use resolution instead", size)
	}
	resolution, ok := viduLegacySizeResolutions[canonical]
	if !ok {
		return "", fmt.Errorf("size %q is not supported by Vidu; use resolution instead", size)
	}
	return resolution, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}

	var taskResp taskResultResponse
	err := common.Unmarshal(respBody, &taskResp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}

	state := taskResp.State
	switch state {
	case "created", "queueing":
		taskInfo.Status = model.TaskStatusSubmitted
	case "processing":
		taskInfo.Status = model.TaskStatusInProgress
	case "success":
		taskInfo.Status = model.TaskStatusSuccess
		if len(taskResp.Creations) > 0 {
			taskInfo.Url = taskResp.Creations[0].URL
		}
	case "failed":
		taskInfo.Status = model.TaskStatusFailure
		if taskResp.ErrCode != "" {
			taskInfo.Reason = taskResp.ErrCode
		}
	default:
		return nil, fmt.Errorf("unknown task state: %s", state)
	}

	return taskInfo, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var viduResp taskResultResponse
	if err := common.Unmarshal(originTask.Data, &viduResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal vidu task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.CompletionTime()

	if len(viduResp.Creations) > 0 && viduResp.Creations[0].URL != "" {
		openAIVideo.SetMetadata("url", viduResp.Creations[0].URL)
	}

	if viduResp.State == "failed" && viduResp.ErrCode != "" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: viduResp.ErrCode,
			Code:    viduResp.ErrCode,
		}
	}

	return common.Marshal(openAIVideo)
}
