package shishi

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
)

// TaskAdaptor implements the platform's universal video contract while keeping
// the public API compatible with OpenAI Video/Sora clients.
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
	if info.Action == constant.TaskActionRemix {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("Shishi Universal does not support video remix"),
			"unsupported_action",
			http.StatusBadRequest,
		)
	}

	if !isJSONRequest(c) {
		return relaycommon.ValidateMultipartDirect(c, info)
	}

	payload, err := requestMap(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	req, hasMedia, err := taskRequestFromPayload(payload, info.OriginModelName)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	c.Set("task_request", req)
	if hasMedia {
		info.Action = constant.TaskActionGenerate
	} else {
		info.Action = constant.TaskActionTextGenerate
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
		seconds = defaultVideoSecond
	}
	sizeRatio := 1.0
	if req.Size == "1792x1024" || req.Size == "1024x1792" {
		sizeRatio = 1.666667
	}
	return map[string]float64{
		"seconds": float64(seconds),
		"size":    sizeRatio,
	}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/v1/videos", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if !isJSONRequest(c) {
		return buildMultipartBody(c, info.UpstreamModelName)
	}

	payload, err := requestMap(c)
	if err != nil {
		return nil, err
	}
	req, err := shishiNormalizedTaskRequest(c, payload, info)
	if err != nil {
		return nil, err
	}
	if err := normalizeSoraContent(payload); err != nil {
		return nil, err
	}
	mapSecondsToDuration(payload)
	if upstreamModelName := strings.TrimSpace(info.UpstreamModelName); upstreamModelName != "" {
		payload["model"] = upstreamModelName
	}
	if _, ok := payload["aspect_ratio"]; !ok {
		if ratio := firstNonEmpty(stringFromValue(payload["aspectRatio"]), stringFromValue(payload["ratio"])); ratio != "" {
			payload["aspect_ratio"] = ratio
		}
	}
	if err := applyCanonicalVideoOutput(payload, req); err != nil {
		return nil, err
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

// mapSecondsToDuration translates the legacy public alias into Shishi's
// documented wire field. An explicitly supplied duration always wins.
func mapSecondsToDuration(payload map[string]any) {
	if _, hasDuration := payload["duration"]; !hasDuration {
		if seconds, hasSeconds := payload["seconds"]; hasSeconds {
			payload["duration"] = seconds
		}
	}
	delete(payload, "seconds")
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

	upstreamResponse := map[string]any{}
	if err := common.Unmarshal(responseBody, &upstreamResponse); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	upstreamTaskID := extractTaskID(upstreamResponse)
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapperLocal(
			fmt.Errorf("Shishi Universal create response has no task id: %s", extractFailureReason(upstreamResponse)),
			"invalid_response",
			http.StatusBadGateway,
		)
	}

	status := mapTaskStatus(extractStatus(upstreamResponse))
	clientResponse := &dto.OpenAIVideo{
		ID:        info.PublicTaskID,
		TaskID:    info.PublicTaskID,
		Object:    "video",
		Model:     firstNonEmpty(info.OriginModelName, stringFromValue(upstreamResponse["model"])),
		Status:    openAIVideoStatus(extractStatus(upstreamResponse)),
		Progress:  upstreamProgress(upstreamResponse),
		CreatedAt: time.Now().Unix(),
	}
	if status == model.TaskStatusFailure {
		clientResponse.Error = &dto.OpenAIVideoError{Message: extractFailureReason(upstreamResponse), Code: "failure"}
	}
	c.JSON(http.StatusOK, clientResponse)
	return upstreamTaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID := stringFromValue(body["task_id"])
	if taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseURL, "/") + "/v1/videos/" + url.PathEscape(taskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(responseBody []byte) (*relaycommon.TaskInfo, error) {
	payload := map[string]any{}
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal Shishi Universal task result: %w", err)
	}

	status := mapTaskStatus(extractStatus(payload))
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown Shishi Universal task status %q", extractStatus(payload))
	}

	result := &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   extractTaskID(payload),
		Status:   string(status),
		Progress: progressValue(payload, status),
	}
	if status == model.TaskStatusSuccess {
		result.Url = extractVideoURL(payload)
		result.Progress = "100%"
	}
	if status == model.TaskStatusFailure {
		result.Reason = extractFailureReason(payload)
		result.Progress = "100%"
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	upstreamResponse := map[string]any{}
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &upstreamResponse); err != nil {
			return nil, fmt.Errorf("unmarshal Shishi Universal task response: %w", err)
		}
	}

	payload := map[string]any{
		"id":         task.TaskID,
		"task_id":    task.TaskID,
		"taskId":     task.TaskID,
		"object":     "video",
		"model":      firstNonEmpty(task.Properties.OriginModelName, task.Properties.UpstreamModelName),
		"status":     openAIVideoStatus(task.Status.ToVideoStatus()),
		"progress":   progressFromTask(task.Progress, task.Status),
		"created_at": task.CreatedAt,
	}
	if task.CreatedAt > 0 {
		payload["created_at"] = task.CreatedAt
	}
	if completedAt := task.CompletionTime(); completedAt > 0 {
		payload["completed_at"] = completedAt
	}

	if videoURL := firstNonEmpty(extractVideoURL(upstreamResponse), task.GetResultURL()); videoURL != "" {
		proxyURL := taskcommon.BuildProxyURL(task.TaskID)
		setProxyVideoURL(payload, proxyURL)
	}
	if task.Status == model.TaskStatusFailure {
		payload["error"] = &dto.OpenAIVideoError{Message: firstNonEmpty(task.FailReason, extractFailureReason(upstreamResponse)), Code: "failure"}
	}
	return common.Marshal(payload)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) BuildPrivateData(_ *gin.Context, info *relaycommon.RelayInfo) (*model.TaskPrivateData, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, fmt.Errorf("channel metadata is missing")
	}
	return &model.TaskPrivateData{Key: info.ApiKey}, nil
}

func isJSONRequest(c *gin.Context) bool {
	return c != nil && c.Request != nil && strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json")
}

func requestMap(c *gin.Context) (map[string]any, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func taskRequestFromPayload(payload map[string]any, fallbackModel string) (relaycommon.TaskSubmitReq, bool, error) {
	prompt := strings.TrimSpace(stringFromValue(payload["prompt"]))
	if prompt == "" {
		return relaycommon.TaskSubmitReq{}, false, fmt.Errorf("prompt is required")
	}
	modelName := firstNonEmpty(stringFromValue(payload["model"]), fallbackModel)
	if modelName == "" {
		return relaycommon.TaskSubmitReq{}, false, fmt.Errorf("model field is required")
	}

	media := collectMedia(payload)
	content, err := taskContent(payload["content"])
	if err != nil {
		return relaycommon.TaskSubmitReq{}, false, err
	}
	for _, item := range content {
		switch item.Type {
		case "image_url":
			if item.ImageURL != nil {
				media.images = append(media.images, item.ImageURL.URL)
			}
		case "video_url":
			if item.VideoURL != nil {
				media.videos = append(media.videos, item.VideoURL.URL)
			}
		case "audio_url":
			if item.AudioURL != nil {
				media.audios = append(media.audios, item.AudioURL.URL)
			}
		}
	}
	// duration is the canonical public spelling. Keep the request stored for
	// billing consistent with the outgoing body, which also gives duration
	// precedence over seconds.
	metadata := taskMetadata(payload["metadata"])
	duration := firstPositiveInt(payload["duration"], payload["seconds"], metadata["duration"])
	req := relaycommon.TaskSubmitReq{
		Prompt:           prompt,
		Model:            modelName,
		Images:           media.images,
		Videos:           media.videos,
		Audios:           media.audios,
		Content:          content,
		Size:             stringFromValue(payload["size"]),
		Ratio:            stringFromValue(payload["ratio"]),
		AspectRatio:      stringFromValue(payload["aspect_ratio"]),
		AspectRatioAlias: stringFromValue(payload["aspectRatio"]),
		Resolution:       stringFromValue(payload["resolution"]),
		Duration:         duration,
		Seconds:          stringFromValue(payload["seconds"]),
		Metadata:         metadata,
	}
	return req, media.hasAny(), nil
}

func shishiNormalizedTaskRequest(c *gin.Context, payload map[string]any, info *relaycommon.RelayInfo) (relaycommon.TaskSubmitReq, error) {
	if _, exists := c.Get("task_request"); exists {
		return relaycommon.GetTaskRequest(c)
	}

	fallbackModel := ""
	if info != nil {
		fallbackModel = info.OriginModelName
	}
	req, _, err := taskRequestFromPayload(payload, fallbackModel)
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	return req, nil
}

func applyCanonicalVideoOutput(payload map[string]any, req relaycommon.TaskSubmitReq) error {
	delete(payload, "ratio")
	delete(payload, "aspectRatio")
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.AspectRatio != "" {
		payload["aspect_ratio"] = req.AspectRatio
	} else {
		delete(payload, "aspect_ratio")
	}
	if req.Resolution != "" {
		payload["resolution"] = req.Resolution
	} else {
		delete(payload, "resolution")
	}
	if req.Metadata == nil {
		return nil
	}
	if _, encoded := payload["metadata"].(string); encoded {
		metadata, err := common.Marshal(req.Metadata)
		if err != nil {
			return err
		}
		payload["metadata"] = string(metadata)
		return nil
	}
	payload["metadata"] = req.Metadata
	return nil
}

type mediaReferences struct {
	images []string
	videos []string
	audios []string
}

func (m mediaReferences) hasAny() bool {
	return len(m.images) > 0 || len(m.videos) > 0 || len(m.audios) > 0
}

func collectMedia(payload map[string]any) mediaReferences {
	media := mediaReferences{
		images: collectURLs(payload, "reference_image_urls", "image_urls", "images", "image_url", "image", "input_reference", "first_frame_url", "first_frame", "last_frame_url", "last_frame"),
		videos: collectURLs(payload, "reference_videos", "video_urls", "videos", "video_url", "video"),
		audios: collectURLs(payload, "reference_audios", "audio_urls", "audios", "audio_url", "audio"),
	}
	for _, key := range []string{"assets", "files"} {
		for _, asset := range arrayValue(payload[key]) {
			item, ok := asset.(map[string]any)
			if !ok {
				continue
			}
			url := stringFromValue(item["url"])
			if url == "" {
				continue
			}
			switch strings.ToLower(stringFromValue(item["type"])) {
			case "image":
				media.images = append(media.images, url)
			case "video":
				media.videos = append(media.videos, url)
			case "audio":
				media.audios = append(media.audios, url)
			}
		}
	}
	return media
}

func collectURLs(payload map[string]any, keys ...string) []string {
	values := make([]string, 0)
	for _, key := range keys {
		appendURLs(&values, payload[key])
	}
	return values
}

func appendURLs(target *[]string, value any) {
	switch typed := value.(type) {
	case string:
		if value := strings.TrimSpace(typed); value != "" {
			*target = append(*target, value)
		}
	case []string:
		for _, item := range typed {
			appendURLs(target, item)
		}
	case []any:
		for _, item := range typed {
			appendURLs(target, item)
		}
	case map[string]any:
		for _, key := range []string{"url", "image_url", "video_url", "audio_url"} {
			if item, ok := typed[key]; ok {
				appendURLs(target, item)
				return
			}
		}
	}
}

func taskContent(value any) ([]relaycommon.TaskContentItem, error) {
	if value == nil {
		return nil, nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var content []relaycommon.TaskContentItem
	if err := common.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	return content, nil
}

// normalizeSoraContent translates Sora's content array into the platform's
// documented universal reference fields. Existing documented fields are kept.
func normalizeSoraContent(payload map[string]any) error {
	content, err := taskContent(payload["content"])
	if err != nil {
		return err
	}
	if len(content) == 0 {
		delete(payload, "content")
		return nil
	}
	media := collectMedia(payload)
	for _, item := range content {
		switch item.Type {
		case "image_url":
			if item.ImageURL != nil {
				media.images = append(media.images, item.ImageURL.URL)
			}
		case "video_url":
			if item.VideoURL != nil {
				media.videos = append(media.videos, item.VideoURL.URL)
			}
		case "audio_url":
			if item.AudioURL != nil {
				media.audios = append(media.audios, item.AudioURL.URL)
			}
		}
	}
	if len(media.images) > 0 {
		payload["reference_image_urls"] = media.images
	}
	if len(media.videos) > 0 {
		payload["reference_videos"] = media.videos
	}
	if len(media.audios) > 0 {
		payload["reference_audios"] = media.audios
	}
	delete(payload, "content")
	return nil
}

func buildMultipartBody(c *gin.Context, upstreamModel string) (io.Reader, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, err
	}
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		if err := relaycommon.ApplyNormalizedTaskMultipartVideoOutput(form.Value, req, relaycommon.MultipartVideoOutputOptions{}); err != nil {
			return nil, err
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", upstreamModel); err != nil {
		return nil, err
	}
	hasDuration := len(form.Value["duration"]) > 0
	for key, values := range form.Value {
		if key == "model" || key == "seconds" {
			continue
		}
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return nil, err
			}
		}
	}
	if !hasDuration {
		for _, value := range form.Value["seconds"] {
			if err := writer.WriteField("duration", value); err != nil {
				return nil, err
			}
		}
	}
	for field, headers := range form.File {
		for _, header := range headers {
			file, err := header.Open()
			if err != nil {
				return nil, err
			}
			contentType := header.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			mimeHeader := make(textproto.MIMEHeader)
			mimeHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, header.Filename))
			mimeHeader.Set("Content-Type", contentType)
			part, err := writer.CreatePart(mimeHeader)
			if err == nil {
				_, err = io.Copy(part, file)
			}
			_ = file.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &body, nil
}

func extractTaskID(payload map[string]any) string {
	return firstStringAtPaths(payload,
		[]string{"id"}, []string{"task_id"}, []string{"taskId"},
		[]string{"data", "id"}, []string{"data", "task_id"}, []string{"data", "taskId"},
	)
}

func extractStatus(payload map[string]any) string {
	return firstStringAtPaths(payload,
		[]string{"status"}, []string{"data", "status"}, []string{"data", "task", "status"}, []string{"task", "status"},
	)
}

func extractVideoURL(payload map[string]any) string {
	keys := []string{"url", "video_url", "videoUrl", "result_url", "downloadUrl", "download_url", "outputVideoUrl", "output_url"}
	if result := firstValueURL(payload, keys...); result != "" {
		return result
	}
	if result := firstValueURL(payload["outputs"], "url", "download_url", "downloadUrl"); result != "" {
		return result
	}
	if result := firstValueURL(payload["generated"], "url", "download_url", "downloadUrl"); result != "" {
		return result
	}
	if result := firstValueURL(payload["output"], "url", "download_url", "downloadUrl"); result != "" {
		return result
	}
	for _, key := range []string{"data", "task", "video", "metadata", "response"} {
		if child := mapValue(payload[key]); child != nil {
			if result := extractVideoURL(child); result != "" {
				return result
			}
		}
	}
	return ""
}

func firstValueURL(value any, keys ...string) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		for _, item := range typed {
			if value := strings.TrimSpace(item); value != "" {
				return value
			}
		}
	case []any:
		for _, item := range typed {
			if value := firstValueURL(item, keys...); value != "" {
				return value
			}
		}
	case map[string]any:
		for _, key := range keys {
			if value := stringFromValue(typed[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func extractFailureReason(payload map[string]any) string {
	for _, path := range [][]string{
		{"failure_reason"}, {"message"}, {"detail"}, {"error"}, {"error", "message"},
		{"data", "failure_reason"}, {"data", "message"}, {"data", "detail"}, {"data", "error"}, {"data", "error", "message"},
	} {
		if value := stringFromValue(valueAtPath(payload, path)); value != "" {
			return value
		}
	}
	return "upstream task failed without an error message"
}

func mapTaskStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending", "submitted", "waiting":
		return model.TaskStatusQueued
	case "processing", "running", "in_progress", "generating":
		return model.TaskStatusInProgress
	case "completed", "complete", "succeeded", "success", "finished", "done":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusUnknown
	}
}

func openAIVideoStatus(status string) string {
	switch mapTaskStatus(status) {
	case model.TaskStatusQueued:
		return dto.VideoStatusQueued
	case model.TaskStatusInProgress:
		return dto.VideoStatusInProgress
	case model.TaskStatusSuccess:
		return dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusQueued
	}
}

func progressValue(payload map[string]any, status model.TaskStatus) string {
	for _, path := range [][]string{{"progress"}, {"data", "progress"}, {"data", "task", "progress"}, {"task", "progress"}} {
		if progress := normalizedProgress(valueAtPath(payload, path)); progress != "" {
			return progress
		}
	}
	switch status {
	case model.TaskStatusSuccess, model.TaskStatusFailure:
		return "100%"
	case model.TaskStatusInProgress:
		return taskcommon.ProgressInProgress
	default:
		return taskcommon.ProgressQueued
	}
}

func upstreamProgress(payload map[string]any) int {
	for _, path := range [][]string{{"progress"}, {"data", "progress"}, {"data", "task", "progress"}, {"task", "progress"}} {
		if progress := normalizedProgress(valueAtPath(payload, path)); progress != "" {
			return progressFromTask(progress, model.TaskStatusUnknown)
		}
	}
	return 0
}

func progressFromTask(progress string, status model.TaskStatus) int {
	if value, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(progress), "%")); err == nil {
		return value
	}
	if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
		return 100
	}
	return 0
}

func setProxyVideoURL(payload map[string]any, proxyURL string) {
	payload["url"] = proxyURL
	payload["video_url"] = proxyURL
	payload["result_url"] = proxyURL
	payload["output_url"] = proxyURL
	payload["output"] = []string{proxyURL}
	payload["outputs"] = []map[string]any{{"url": proxyURL, "download_url": proxyURL, "type": "video"}}
	payload["video"] = map[string]any{"url": proxyURL}
}

func firstStringAtPaths(payload map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value := stringFromValue(valueAtPath(payload, path)); value != "" {
			return value
		}
	}
	return ""
}

func valueAtPath(payload map[string]any, path []string) any {
	var value any = payload
	for _, key := range path {
		mapValue, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = mapValue[key]
	}
	return value
}

func nestedValue(payload map[string]any, parent, key string) any {
	return valueAtPath(payload, []string{parent, key})
}

func mapValue(value any) map[string]any {
	mapValue, _ := value.(map[string]any)
	return mapValue
}

func taskMetadata(value any) map[string]any {
	if metadata := mapValue(value); metadata != nil {
		return metadata
	}
	rawMetadata, ok := value.(string)
	if !ok || strings.TrimSpace(rawMetadata) == "" {
		return nil
	}
	var metadata map[string]any
	if err := common.UnmarshalJsonStr(rawMetadata, &metadata); err != nil {
		return nil
	}
	return metadata
}

func arrayValue(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func stringFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		if seconds, ok := positiveInt(value); ok {
			return seconds
		}
	}
	return 0
}

func positiveInt(value any) (int, bool) {
	valueString := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(stringFromValue(value))), "seconds")
	valueString = strings.TrimSuffix(valueString, "second")
	valueString = strings.TrimSuffix(valueString, "secs")
	valueString = strings.TrimSuffix(valueString, "sec")
	valueString = strings.TrimSuffix(valueString, "s")
	parsed, err := strconv.ParseFloat(strings.TrimSpace(valueString), 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return int(parsed), true
}

func normalizedProgress(value any) string {
	progress := strings.TrimSuffix(stringFromValue(value), "%")
	if progress == "" {
		return ""
	}
	parsed, err := strconv.Atoi(progress)
	if err != nil || parsed < 0 {
		return ""
	}
	if parsed > 100 {
		parsed = 100
	}
	return strconv.Itoa(parsed) + "%"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
