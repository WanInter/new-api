package jimengdimensio

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
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
)

type jsonRequest struct {
	Prompt         string         `json:"prompt"`
	Model          string         `json:"model,omitempty"`
	Image          string         `json:"image,omitempty"`
	Images         []string       `json:"images,omitempty"`
	Size           string         `json:"size,omitempty"`
	Duration       int            `json:"duration,omitempty"`
	Seconds        string         `json:"seconds,omitempty"`
	FilePaths      []string       `json:"file_paths,omitempty"`
	Ratio          string         `json:"ratio,omitempty"`
	AspectRatio    string         `json:"aspect_ratio,omitempty"`
	Resolution     string         `json:"resolution,omitempty"`
	FunctionMode   string         `json:"functionMode,omitempty"`
	ResponseFormat string         `json:"response_format,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type requestPayload struct {
	Model          string   `json:"model"`
	Prompt         string   `json:"prompt"`
	Ratio          string   `json:"ratio,omitempty"`
	Resolution     string   `json:"resolution,omitempty"`
	Duration       int      `json:"duration,omitempty"`
	FilePaths      []string `json:"file_paths,omitempty"`
	ImageFile1     string   `json:"image_file_1,omitempty"`
	ImageFile2     string   `json:"image_file_2,omitempty"`
	ImageFile3     string   `json:"image_file_3,omitempty"`
	ImageFile4     string   `json:"image_file_4,omitempty"`
	ImageFile5     string   `json:"image_file_5,omitempty"`
	ImageFile6     string   `json:"image_file_6,omitempty"`
	ImageFile7     string   `json:"image_file_7,omitempty"`
	ImageFile8     string   `json:"image_file_8,omitempty"`
	ImageFile9     string   `json:"image_file_9,omitempty"`
	FunctionMode   string   `json:"functionMode,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

type submitResponse struct {
	Created int64  `json:"created"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"error_code,omitempty"`
}

type taskResponse struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Progress int    `json:"progress,omitempty"`
	Result   struct {
		URL    string `json:"url"`
		B64URL string `json:"b64_url"`
	} `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
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
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := validateRequest(c, info); err != nil {
		return err
	}
	info.Action = constant.TaskActionGenerate
	return nil
}

func validateRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req, err := parseMultipartRequest(c)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_multipart_form", http.StatusBadRequest)
		}
		if strings.TrimSpace(req.Model) == "" {
			req.Model = info.OriginModelName
		}
		if strings.TrimSpace(req.Model) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
		}
		if strings.TrimSpace(req.Prompt) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
		}
		c.Set("task_request", req)
		return nil
	}

	var raw jsonRequest
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	req := relaycommon.TaskSubmitReq{
		Prompt:   raw.Prompt,
		Model:    raw.Model,
		Image:    raw.Image,
		Images:   raw.Images,
		Size:     raw.Size,
		Duration: raw.Duration,
		Seconds:  raw.Seconds,
		Metadata: raw.Metadata,
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = info.OriginModelName
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if len(req.Images) == 0 && len(req.Image) > 0 {
		req.Images = []string{req.Image}
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	copyJSONRequestMetadata(raw, req.Metadata)
	c.Set("task_request", req)
	return nil
}

func copyJSONRequestMetadata(raw jsonRequest, metadata map[string]any) {
	if raw.Ratio != "" {
		metadata["ratio"] = raw.Ratio
	}
	if raw.AspectRatio != "" {
		metadata["aspect_ratio"] = raw.AspectRatio
	}
	if raw.Metadata != nil {
		if v, ok := raw.Metadata["aspect_ratio"]; ok {
			metadata["aspect_ratio"] = v
		}
	}
	if raw.Resolution != "" {
		metadata["resolution"] = raw.Resolution
	}
	if raw.FunctionMode != "" {
		metadata["functionMode"] = raw.FunctionMode
	}
	if raw.ResponseFormat != "" {
		metadata["response_format"] = raw.ResponseFormat
	}
	if len(raw.FilePaths) > 0 {
		metadata["file_paths"] = raw.FilePaths
	}
	if raw.Seconds != "" && raw.Duration == 0 {
		if seconds := parsePositiveInt(raw.Seconds, 0); seconds > 0 {
			metadata["duration"] = seconds
		}
	}
}

func parseMultipartRequest(c *gin.Context) (relaycommon.TaskSubmitReq, error) {
	if _, err := c.MultipartForm(); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	form := c.Request.PostForm
	req := relaycommon.TaskSubmitReq{
		Model:    form.Get("model"),
		Prompt:   form.Get("prompt"),
		Duration: parsePositiveInt(form.Get("duration"), 0),
		Metadata: map[string]any{},
	}
	for _, key := range []string{"ratio", "aspect_ratio", "resolution", "functionMode", "response_format"} {
		if v := strings.TrimSpace(form.Get(key)); v != "" {
			req.Metadata[key] = v
		}
	}
	if paths := form["file_paths"]; len(paths) > 0 {
		req.Metadata["file_paths"] = paths
	}
	return req, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	payload, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil
	}
	return map[string]float64{
		"seconds":    float64(payload.Duration),
		"resolution": resolutionRatio(payload.Model, payload.Resolution),
	}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/videos/generations", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		req.Header.Set("Content-Type", "application/json")
	}
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

	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		return a.buildMultipartBody(c, &req, info)
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

func (a *TaskAdaptor) buildMultipartBody(c *gin.Context, req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (io.Reader, error) {
	payload, err := a.convertToRequestPayload(req, info)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeField := func(k, v string) error {
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return writer.WriteField(k, v)
	}
	if err := writeField("model", payload.Model); err != nil {
		return nil, err
	}
	if err := writeField("prompt", payload.Prompt); err != nil {
		return nil, err
	}
	if err := writeField("ratio", payload.Ratio); err != nil {
		return nil, err
	}
	if err := writeField("resolution", payload.Resolution); err != nil {
		return nil, err
	}
	if payload.Duration > 0 {
		if err := writer.WriteField("duration", strconv.Itoa(payload.Duration)); err != nil {
			return nil, err
		}
	}
	if err := writeField("functionMode", payload.FunctionMode); err != nil {
		return nil, err
	}
	if err := writeField("response_format", payload.ResponseFormat); err != nil {
		return nil, err
	}
	for _, p := range payload.FilePaths {
		if err := writer.WriteField("file_paths", p); err != nil {
			return nil, err
		}
	}
	if err := copyAllowedFormFiles(c, writer); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return bytes.NewReader(body.Bytes()), nil
}

func copyAllowedFormFiles(c *gin.Context, writer *multipart.Writer) error {
	mf, err := c.MultipartForm()
	if err != nil {
		return err
	}
	for prefix, max := range map[string]int{"image_file_": 9, "video_file_": 3, "audio_file_": 3} {
		for i := 1; i <= max; i++ {
			field := fmt.Sprintf("%s%d", prefix, i)
			for _, fh := range mf.File[field] {
				if err := copyFormFile(writer, field, fh); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func copyFormFile(writer *multipart.Writer, field string, fh *multipart.FileHeader) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := writer.CreateFormFile(field, fh.Filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var submit submitResponse
	if err := common.Unmarshal(responseBody, &submit); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if submit.TaskID == "" || submit.Status == "failed" {
		msg := submit.Error
		if msg == "" {
			msg = "submit task failed"
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", msg), defaultString(submit.Code, "submit_failed"), http.StatusBadRequest)
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	if submit.Created > 0 {
		ov.CreatedAt = submit.Created
	}
	ov.Model = info.OriginModelName
	ov.Status = mapVideoStatus(submit.Status)
	c.JSON(http.StatusOK, ov)
	return submit.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/videos/tasks/%s", strings.TrimRight(baseUrl, "/"), taskID)
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
	var res taskResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	info := relaycommon.TaskInfo{}
	if res.Error != "" || res.Status == "failed" {
		info.Code = parseErrorCode(res.ErrorCode)
		info.Reason = res.Error
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		return &info, nil
	}
	info.Code = 0
	switch res.Status {
	case "pending":
		info.Status = model.TaskStatusQueued
		info.Progress = "10%"
	case "processing":
		info.Status = model.TaskStatusInProgress
		info.Progress = progressString(res.Progress, "50%")
	case "completed":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
		info.Url = res.Result.URL
	case "not_found":
		info.Code = http.StatusNotFound
		info.Reason = "task not found or expired"
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
	default:
		info.Status = model.TaskStatusInProgress
		info.Progress = progressString(res.Progress, "30%")
	}
	return &info, nil
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	openAIVideo := originTask.ToOpenAIVideo()
	var res taskResponse
	if len(originTask.Data) > 0 {
		_ = common.Unmarshal(originTask.Data, &res)
	}
	if res.Result.URL != "" {
		openAIVideo.SetMetadata("url", res.Result.URL)
	}
	if res.Result.B64URL != "" {
		openAIVideo.SetMetadata("b64_url", res.Result.B64URL)
	}
	if res.Error != "" || originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{Message: defaultString(res.Error, originTask.FailReason), Code: res.ErrorCode}
	}
	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	payload := &requestPayload{
		Model:      info.UpstreamModelName,
		Prompt:     req.Prompt,
		Ratio:      DefaultRatio,
		Resolution: DefaultResolution,
		Duration:   DefaultDuration,
	}
	if req.Model != "" && !info.IsModelMapped {
		payload.Model = req.Model
	}
	if req.Duration > 0 {
		payload.Duration = req.Duration
	}
	if req.Size != "" {
		payload.Resolution = normalizeResolution(req.Size)
	}
	if len(req.Images) > 0 {
		payload.FilePaths = append(payload.FilePaths, req.Images...)
	}
	if aspectRatio, ok := req.Metadata["aspect_ratio"].(string); ok && strings.TrimSpace(aspectRatio) != "" {
		payload.Ratio = aspectRatio
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, payload); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}
	if payload.Model == "" {
		payload.Model = info.OriginModelName
	}
	if payload.Ratio == "" {
		payload.Ratio = DefaultRatio
	}
	if payload.Resolution == "" {
		payload.Resolution = DefaultResolution
	}
	payload.Resolution = normalizeResolution(payload.Resolution)
	if payload.Duration <= 0 {
		payload.Duration = DefaultDuration
	}
	applyReferenceFilesAndMode(payload)
	return payload, nil
}

func applyReferenceFilesAndMode(payload *requestPayload) {
	for i, path := range payload.FilePaths {
		if i >= 9 {
			break
		}
		switch i {
		case 0:
			payload.ImageFile1 = path
		case 1:
			payload.ImageFile2 = path
		case 2:
			payload.ImageFile3 = path
		case 3:
			payload.ImageFile4 = path
		case 4:
			payload.ImageFile5 = path
		case 5:
			payload.ImageFile6 = path
		case 6:
			payload.ImageFile7 = path
		case 7:
			payload.ImageFile8 = path
		case 8:
			payload.ImageFile9 = path
		}
	}
	payload.FilePaths = nil
	if strings.TrimSpace(payload.FunctionMode) != "" {
		return
	}
	imageCount := 0
	for _, path := range []string{payload.ImageFile1, payload.ImageFile2, payload.ImageFile3, payload.ImageFile4, payload.ImageFile5, payload.ImageFile6, payload.ImageFile7, payload.ImageFile8, payload.ImageFile9} {
		if path != "" {
			imageCount++
		}
	}
	if imageCount > 2 {
		payload.FunctionMode = "omni_reference"
		return
	}
	payload.FunctionMode = "first_last_frames"
}

func resolutionRatio(modelName, resolution string) float64 {
	resolution = normalizeResolution(resolution)
	if modelName == "jimeng-video-seedance-2.0-vip" && resolution == "1080p" {
		return 2.25
	}
	return 1
}

func normalizeResolution(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.Contains(v, "1080"):
		return "1080p"
	case strings.Contains(v, "720"):
		return "720p"
	default:
		return v
	}
}

func mapVideoStatus(status string) string {
	switch status {
	case "pending":
		return dto.VideoStatusQueued
	case "processing":
		return dto.VideoStatusInProgress
	case "completed":
		return dto.VideoStatusCompleted
	case "failed", "not_found":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusQueued
	}
}

func progressString(progress int, fallback string) string {
	if progress > 0 {
		return fmt.Sprintf("%d%%", progress)
	}
	return fallback
}

func parsePositiveInt(s string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func parseErrorCode(code string) int {
	v, err := strconv.Atoi(code)
	if err != nil || v == 0 {
		return http.StatusBadRequest
	}
	return v
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
