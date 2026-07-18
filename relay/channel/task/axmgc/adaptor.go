package axmgc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
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
	ChannelName             = "axmgc"
	DefaultBaseURL          = "https://axmgc.com"
	Seedance720p933Model    = "seedance-2-720p-933"
	defaultDuration         = 15
	defaultResolution       = "720p"
	maxImages               = 9
	maxVideos               = 3
	maxAudios               = 3
	multipartFormContextKey = "axmgc_multipart_form"
)

var ModelList = []string{Seedance720p933Model}

type requestPayload struct {
	Model       string                        `json:"model"`
	Content     []relaycommon.TaskContentItem `json:"content,omitempty"`
	AspectRatio string                        `json:"aspect_ratio,omitempty"`
	Resolution  string                        `json:"resolution,omitempty"`
	Duration    int                           `json:"duration"`
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
	apiKey    string
	baseURL   string
	multipart bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
	a.multipart = false
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info != nil && info.Action == constant.TaskActionRemix {
		return service.TaskErrorWrapperLocal(errors.New("Axmgc does not support video remix"), "unsupported_action", http.StatusBadRequest)
	}
	a.multipart = strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data")
	if a.multipart {
		return a.validateMultipartRequest(c, info)
	}
	return a.validateJSONRequest(c, info)
}

func (a *TaskAdaptor) validateJSONRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var raw relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	content := raw.Content
	if len(content) == 0 {
		content = contentFromURLs(raw)
		if prompt := strings.TrimSpace(raw.Prompt); prompt != "" {
			content = append(content, relaycommon.TaskContentItem{Type: "text", Text: prompt})
		}
	} else if !contentHasText(content) && strings.TrimSpace(raw.Prompt) != "" {
		content = append(content, relaycommon.TaskContentItem{Type: "text", Text: strings.TrimSpace(raw.Prompt)})
	}

	prompt, images, videos, audios, err := validateContent(content)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if err := validateResolution(raw.Resolution); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(firstNonEmpty(raw.Model, info.OriginModelName)) == "" {
		return service.TaskErrorWrapperLocal(errors.New("model field is required"), "missing_model", http.StatusBadRequest)
	}

	raw.Model = firstNonEmpty(raw.Model, info.OriginModelName)
	raw.Prompt = prompt
	raw.Content = content
	raw.Images = images
	raw.Videos = videos
	raw.Audios = audios
	raw.AspectRatio = strings.TrimSpace(raw.AspectRatio)
	raw.Resolution = strings.TrimSpace(raw.Resolution)
	raw.Duration = defaultDuration
	storeValidatedTaskRequest(c, info, raw, len(images)+len(videos)+len(audios) > 0)
	return nil
}

func (a *TaskAdaptor) validateMultipartRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_multipart_form", http.StatusBadRequest)
	}
	fail := func(err error, code string) *dto.TaskError {
		_ = form.RemoveAll()
		return service.TaskErrorWrapperLocal(err, code, http.StatusBadRequest)
	}

	prompt := strings.TrimSpace(firstFormValue(form, "prompt"))
	if prompt == "" {
		return fail(errors.New("prompt is required"), "invalid_request")
	}
	modelName := firstNonEmpty(firstFormValue(form, "model"), info.OriginModelName)
	if strings.TrimSpace(modelName) == "" {
		return fail(errors.New("model field is required"), "missing_model")
	}
	resolution := strings.TrimSpace(firstFormValue(form, "resolution"))
	if err := validateResolution(resolution); err != nil {
		return fail(err, "invalid_request")
	}
	images := countFiles(form, "images", "image")
	videos := countFiles(form, "videos", "video")
	audios := countFiles(form, "audios", "audio")
	if err := validateMediaCounts(images, videos, audios); err != nil {
		return fail(err, "invalid_request")
	}

	c.Set(multipartFormContextKey, form)
	req := relaycommon.TaskSubmitReq{
		Model:       modelName,
		Prompt:      prompt,
		AspectRatio: strings.TrimSpace(firstFormValue(form, "aspect_ratio")),
		Resolution:  resolution,
		Duration:    defaultDuration,
	}
	storeValidatedTaskRequest(c, info, req, images+videos+audios > 0)
	return nil
}

func storeValidatedTaskRequest(c *gin.Context, info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq, hasReferences bool) {
	info.Action = constant.TaskActionTextGenerate
	if hasReferences {
		info.Action = constant.TaskActionGenerate
	}
	c.Set("task_request", req)
}

func contentFromURLs(raw relaycommon.TaskSubmitReq) []relaycommon.TaskContentItem {
	content := make([]relaycommon.TaskContentItem, 0, len(raw.Images)+len(raw.ImageURLs)+len(raw.Videos)+len(raw.VideoURLs)+len(raw.Audios)+len(raw.AudioURLs)+3)
	for _, url := range appendNonEmpty([]string{raw.Image}, raw.Images, raw.ImageURLs, raw.InputStartFrames, raw.InputImageReferences) {
		content = append(content, relaycommon.TaskContentItem{Type: "image_url", ImageURL: &relaycommon.TaskContentURL{URL: url}})
	}
	for _, url := range appendNonEmpty(raw.Videos, raw.VideoURLs) {
		content = append(content, relaycommon.TaskContentItem{Type: "video_url", VideoURL: &relaycommon.TaskContentURL{URL: url}})
	}
	for _, url := range appendNonEmpty(raw.Audios, raw.AudioURLs) {
		content = append(content, relaycommon.TaskContentItem{Type: "audio_url", AudioURL: &relaycommon.TaskContentURL{URL: url}})
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

func contentHasText(content []relaycommon.TaskContentItem) bool {
	for _, item := range content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return true
		}
	}
	return false
}

func validateContent(content []relaycommon.TaskContentItem) (string, []string, []string, []string, error) {
	if len(content) == 0 {
		return "", nil, nil, nil, errors.New("content with a text item is required")
	}
	var prompts, images, videos, audios []string
	seenText := false
	for _, item := range content {
		switch item.Type {
		case "text":
			if text := strings.TrimSpace(item.Text); text != "" {
				prompts = append(prompts, text)
				seenText = true
			}
		case "image_url":
			if seenText {
				return "", nil, nil, nil, errors.New("reference content must appear before text content")
			}
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return "", nil, nil, nil, errors.New("image_url.url is required")
			}
			images = append(images, strings.TrimSpace(item.ImageURL.URL))
		case "video_url":
			if seenText {
				return "", nil, nil, nil, errors.New("reference content must appear before text content")
			}
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" {
				return "", nil, nil, nil, errors.New("video_url.url is required")
			}
			videos = append(videos, strings.TrimSpace(item.VideoURL.URL))
		case "audio_url":
			if seenText {
				return "", nil, nil, nil, errors.New("reference content must appear before text content")
			}
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" {
				return "", nil, nil, nil, errors.New("audio_url.url is required")
			}
			audios = append(audios, strings.TrimSpace(item.AudioURL.URL))
		default:
			return "", nil, nil, nil, fmt.Errorf("unsupported content type %q", item.Type)
		}
	}
	if len(prompts) == 0 {
		return "", nil, nil, nil, errors.New("content must contain a non-empty text item")
	}
	if err := validateMediaCounts(len(images), len(videos), len(audios)); err != nil {
		return "", nil, nil, nil, err
	}
	return strings.Join(prompts, "\n"), images, videos, audios, nil
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

func countFiles(form *multipart.Form, fields ...string) int {
	count := 0
	for _, field := range fields {
		count += len(form.File[field])
	}
	return count
}

func firstFormValue(form *multipart.Form, field string) string {
	if form == nil || len(form.Value[field]) == 0 {
		return ""
	}
	return form.Value[field][0]
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info != nil && info.Action == constant.TaskActionRemix {
		return "", errors.New("Axmgc does not support video remix")
	}
	path := "/v1/video/generations"
	if a.multipart {
		path += "/multipart"
	}
	return a.baseURL + path, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Accept", "application/json")
	if a.multipart {
		req.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey := strings.TrimSpace(c.GetHeader("X-Idempotency-Key")); idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.multipart {
		return a.buildMultipartRequestBody(c, info)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	payload := requestPayload{
		Model:       upstreamModelName(info, req.Model),
		Content:     req.Content,
		AspectRatio: req.AspectRatio,
		Resolution:  firstNonEmpty(req.Resolution, defaultResolution),
		Duration:    defaultDuration,
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
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

func (a *TaskAdaptor) buildMultipartRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	form, ok := c.Get(multipartFormContextKey)
	if !ok {
		parsed, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		form = parsed
	}
	multipartForm, ok := form.(*multipart.Form)
	if !ok || multipartForm == nil {
		return nil, errors.New("multipart form is unavailable")
	}
	defer multipartForm.RemoveAll()

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range map[string]string{
		"model":        upstreamModelName(info, req.Model),
		"prompt":       req.Prompt,
		"aspect_ratio": req.AspectRatio,
		"resolution":   firstNonEmpty(req.Resolution, defaultResolution),
		"duration":     strconv.Itoa(defaultDuration),
	} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	for _, spec := range []struct {
		Target string
		Fields []string
	}{
		{Target: "images", Fields: []string{"images", "image"}},
		{Target: "videos", Fields: []string{"videos", "video"}},
		{Target: "audios", Fields: []string{"audios", "audio"}},
	} {
		if err := copyMultipartFiles(writer, multipartForm, spec.Target, spec.Fields...); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &buffer, nil
}

func copyMultipartFiles(writer *multipart.Writer, form *multipart.Form, target string, fields ...string) error {
	for _, field := range fields {
		for _, fileHeader := range form.File[field] {
			file, err := fileHeader.Open()
			if err != nil {
				return err
			}
			contentType := fileHeader.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, target, fileHeader.Filename))
			header.Set("Content-Type", contentType)
			part, err := writer.CreatePart(header)
			if err == nil {
				_, err = io.Copy(part, file)
			}
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func upstreamModelName(info *relaycommon.RelayInfo, fallback string) string {
	if info != nil {
		if name := strings.TrimSpace(info.UpstreamModelName); name != "" {
			return name
		}
		if info.ChannelMeta != nil {
			if name := strings.TrimSpace(info.ChannelMeta.UpstreamModelName); name != "" {
				return name
			}
		}
	}
	return firstNonEmpty(fallback, Seedance720p933Model)
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
