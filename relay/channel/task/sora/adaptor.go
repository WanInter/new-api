package sora

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

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
	canvasStandardSeedanceModel   = "navos-local-seedance-154-36-180-7"
	canvasStandardMaxVideoSeconds = 14
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // for text type
	ImageURL *ImageURL `json:"image_url,omitempty"` // for image_url type
}

type ImageURL struct {
	URL string `json:"url"`
}

type responseTask struct {
	ID                 string `json:"id"`
	TaskID             string `json:"task_id,omitempty"` //兼容旧接口
	Object             string `json:"object"`
	Model              string `json:"model"`
	Status             string `json:"status"`
	Progress           int    `json:"progress"`
	CreatedAt          int64  `json:"created_at"`
	CompletedAt        int64  `json:"completed_at,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	Seconds            string `json:"seconds,omitempty"`
	Size               string `json:"size,omitempty"`
	RemixedFromVideoID string `json:"remixed_from_video_id,omitempty"`
	ResultURL          string `json:"result_url,omitempty"`
	VideoURL           string `json:"video_url,omitempty"`
	URL                string `json:"url,omitempty"`
	Output             any    `json:"output,omitempty"`
	Video              *struct {
		URL string `json:"url,omitempty"`
	} `json:"video,omitempty"`
	Error   *responseTaskError `json:"error,omitempty"`
	Detail  any                `json:"detail,omitempty"`
	Message string             `json:"message,omitempty"`
}

type responseTaskError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *responseTaskError) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		return nil
	}

	var message string
	if err := common.Unmarshal(data, &message); err == nil {
		e.Message = message
		return nil
	}

	type alias responseTaskError
	var parsed alias
	if err := common.Unmarshal(data, &parsed); err != nil {
		return err
	}
	e.Message = parsed.Message
	e.Code = parsed.Code
	return nil
}

func extractResponseTaskVideoURL(task responseTask) string {
	for _, candidate := range []string{task.VideoURL, task.ResultURL, task.URL} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	if task.Video != nil && strings.TrimSpace(task.Video.URL) != "" {
		return strings.TrimSpace(task.Video.URL)
	}
	return extractVideoURLFromAny(task.Output)
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

func validateRemixRequest(c *gin.Context) *dto.TaskError {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	// 存储原始请求到 context，与 ValidateMultipartDirect 路径保持一致
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if info.Action == constant.TaskActionRemix {
		return validateRemixRequest(c)
	}
	return relaycommon.ValidateMultipartDirect(c, info)
}

// EstimateBilling 根据用户请求的 seconds 和 size 计算 OtherRatios。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	// remix 路径的 OtherRatios 已在 ResolveOriginTask 中设置
	if info.Action == constant.TaskActionRemix {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	seconds := estimateVideoSeconds(req, info)
	size := req.Size
	if size == "" {
		size = "720x1280"
	}

	ratios := map[string]float64{
		"seconds": float64(seconds),
		"size":    1,
	}
	if size == "1792x1024" || size == "1024x1792" {
		ratios["size"] = 1.666667
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.Action == constant.TaskActionRemix {
		return fmt.Sprintf("%s/v1/videos/%s/remix", a.baseURL, info.OriginTaskID), nil
	}
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}
	contentType := c.GetHeader("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		var bodyMap map[string]interface{}
		if err := common.Unmarshal(cachedBody, &bodyMap); err == nil {
			bodyMap["model"] = info.UpstreamModelName
			if shouldUseOtoySeedanceMiniReference(info) {
				applyOtoySeedanceMiniReferenceRequest(bodyMap)
			} else {
				if shouldUseVeoReferenceImages(info, bodyMap) {
					applyVeoReferenceImages(bodyMap)
				}
				maxSeconds := maxVideoSecondsForModel(info)
				clampJSONVideoDuration(bodyMap, maxSeconds)
				if seconds, ok := normalizeVideoSeconds(bodyMap["seconds"]); ok {
					bodyMap["seconds"] = clampNormalizedVideoSeconds(seconds, maxSeconds)
				} else if seconds, ok := normalizeVideoSeconds(bodyMap["duration"]); ok {
					bodyMap["seconds"] = clampNormalizedVideoSeconds(seconds, maxSeconds)
				}
			}
			if newBody, err := common.Marshal(bodyMap); err == nil {
				return bytes.NewReader(newBody), nil
			}
		}
		return bytes.NewReader(cachedBody), nil
	}

	if strings.Contains(contentType, "multipart/form-data") {
		formData, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return bytes.NewReader(cachedBody), nil
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("model", info.UpstreamModelName)
		if shouldUseOtoySeedanceMiniReference(info) {
			writeOtoySeedanceMiniReferenceMultipartFields(writer, formData.Value)
		} else {
			maxSeconds := maxVideoSecondsForModel(info)
			if seconds := normalizeVideoSecondsFromForm(formData.Value); seconds != "" {
				writer.WriteField("seconds", clampNormalizedVideoSeconds(seconds, maxSeconds))
			}
			for key, values := range formData.Value {
				if key == "model" || key == "seconds" {
					continue
				}
				for _, v := range values {
					if key == "duration" {
						v = clampRawVideoSeconds(v, maxSeconds)
					}
					writer.WriteField(key, v)
				}
			}
		}
		for fieldName, fileHeaders := range formData.File {
			for _, fh := range fileHeaders {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				ct := fh.Header.Get("Content-Type")
				if ct == "" || ct == "application/octet-stream" {
					buf512 := make([]byte, 512)
					n, _ := io.ReadFull(f, buf512)
					ct = http.DetectContentType(buf512[:n])
					// Re-open after sniffing so the full content is copied below
					f.Close()
					f, err = fh.Open()
					if err != nil {
						continue
					}
				}
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fh.Filename))
				h.Set("Content-Type", ct)
				part, err := writer.CreatePart(h)
				if err != nil {
					f.Close()
					continue
				}
				io.Copy(part, f)
				f.Close()
			}
		}
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &buf, nil
	}

	return common.ReaderOnly(storage), nil
}

func writeOtoySeedanceMiniReferenceMultipartFields(writer *multipart.Writer, values map[string][]string) {
	writeValues := func(key string, vals []string) {
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if v != "" {
				_ = writer.WriteField(key, v)
			}
		}
	}

	allowedPassthrough := map[string]bool{
		"prompt":         true,
		"type":           true,
		"video_urls":     true,
		"audio_urls":     true,
		"resolution":     true,
		"aspect_ratio":   true,
		"generate_audio": true,
		"end_user_id":    true,
	}
	for key, vals := range values {
		if allowedPassthrough[key] {
			writeValues(key, vals)
		}
	}

	if len(values["aspect_ratio"]) == 0 {
		writeValues("aspect_ratio", values["ratio"])
	}
	if duration, ok := normalizeVideoDurationString(firstFormValue(values, "duration")); ok {
		_ = writer.WriteField("duration", duration)
	} else if duration, ok := normalizeVideoDurationString(firstFormValue(values, "seconds")); ok {
		_ = writer.WriteField("duration", duration)
	}

	imageValues := append([]string{}, values["image_urls"]...)
	imageValues = append(imageValues, values["images"]...)
	imageValues = append(imageValues, values["image"]...)
	imageValues = append(imageValues, values["input_reference"]...)
	imageValues = append(imageValues, values["file_paths"]...)
	writeValues("image_urls", uniqueStrings(imageValues))

	if len(values["type"]) == 0 {
		_ = writer.WriteField("type", "image-to-video")
	}
	if len(values["generate_audio"]) == 0 {
		_ = writer.WriteField("generate_audio", "true")
	}
}

func firstFormValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func shouldUseOtoySeedanceMiniReference(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	return isOtoySeedanceMiniReferenceModel(info.OriginModelName) || isOtoySeedanceMiniReferenceModel(info.UpstreamModelName)
}

func isOtoySeedanceMiniReferenceModel(modelName string) bool {
	return strings.TrimSpace(modelName) == "otoy-image-to-video-seedance-2-0-mini-reference-to-video"
}

func applyOtoySeedanceMiniReferenceRequest(body map[string]interface{}) {
	if body == nil {
		return
	}

	if _, exists := body["aspect_ratio"]; !exists {
		if ratio, ok := body["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
			body["aspect_ratio"] = strings.TrimSpace(ratio)
		}
	}

	delete(body, "seconds")
	if duration, ok := normalizeVideoDurationString(body["duration"]); ok {
		body["duration"] = duration
	}

	images := collectOtoySeedanceMiniReferenceImages(body)
	if len(images) > 0 {
		body["image_urls"] = images
	}

	if _, exists := body["type"]; !exists {
		body["type"] = "image-to-video"
	}
	if _, exists := body["generate_audio"]; !exists {
		body["generate_audio"] = true
	}

	keep := map[string]bool{
		"model":          true,
		"prompt":         true,
		"type":           true,
		"image_urls":     true,
		"video_urls":     true,
		"audio_urls":     true,
		"resolution":     true,
		"duration":       true,
		"aspect_ratio":   true,
		"generate_audio": true,
		"end_user_id":    true,
	}
	for key := range body {
		if !keep[key] {
			delete(body, key)
		}
	}
}

func normalizeVideoDurationString(value any) (string, bool) {
	if duration, ok := normalizeVideoSeconds(value); ok {
		return duration, true
	}
	if s, ok := value.(string); ok {
		s = strings.TrimSpace(s)
		if strings.EqualFold(s, "auto") {
			return "auto", true
		}
	}
	return "", false
}

func collectOtoySeedanceMiniReferenceImages(body map[string]interface{}) []string {
	if body == nil {
		return nil
	}
	images := make([]string, 0)
	seen := make(map[string]bool)
	appendImage := func(image string) {
		image = strings.TrimSpace(image)
		if image == "" || seen[image] {
			return
		}
		seen[image] = true
		images = append(images, image)
	}
	appendImages := func(v any) {
		switch typed := v.(type) {
		case []string:
			for _, image := range typed {
				appendImage(image)
			}
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					appendImage(s)
				}
			}
		case string:
			appendImage(typed)
		}
	}

	appendImages(body["image_urls"])
	appendImages(body["images"])
	appendImages(body["image"])
	appendImages(body["input_reference"])
	appendImages(body["file_paths"])
	return images
}

func shouldUseVeoReferenceImages(info *relaycommon.RelayInfo, body map[string]interface{}) bool {
	if info == nil {
		return false
	}
	if strings.TrimSpace(info.OriginModelName) != "veo-omni-flash" &&
		strings.TrimSpace(info.UpstreamModelName) != "veo-omni-flash" {
		return false
	}
	return len(collectVeoImages(body)) > 0
}

func applyVeoReferenceImages(body map[string]interface{}) {
	images := collectVeoImages(body)
	if len(images) > 2 {
		body["Ingredients_images"] = images
		delete(body, "images")
	} else if len(images) > 0 {
		body["images"] = images
		delete(body, "Ingredients_images")
	}
	delete(body, "image")
	delete(body, "input_reference")
}

func collectVeoImages(body map[string]interface{}) []string {
	if body == nil {
		return nil
	}
	images := make([]string, 0)
	seen := make(map[string]bool)
	appendImage := func(image string) {
		image = strings.TrimSpace(image)
		if image == "" || seen[image] {
			return
		}
		seen[image] = true
		images = append(images, image)
	}
	appendImages := func(v any) {
		switch typed := v.(type) {
		case []string:
			for _, image := range typed {
				appendImage(image)
			}
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					appendImage(s)
				}
			}
		case string:
			appendImage(typed)
		}
	}

	appendImages(body["Ingredients_images"])
	appendImages(body["images"])
	appendImages(body["image"])
	appendImages(body["input_reference"])
	return images
}

func estimateVideoSeconds(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) int {
	seconds := 0
	if normalized, ok := normalizeVideoSeconds(req.Seconds); ok {
		seconds = mustParsePositiveInt(normalized)
	} else if req.Duration > 0 {
		seconds = req.Duration
	} else if normalized, ok := normalizeVideoSeconds(req.Metadata["duration"]); ok {
		seconds = mustParsePositiveInt(normalized)
	} else if isSeedanceGateway(info, req.Model) {
		seconds = 15
	} else {
		seconds = 4
	}
	return clampVideoSeconds(seconds, maxVideoSecondsForModel(info))
}

func isSeedanceGateway(info *relaycommon.RelayInfo, modelName string) bool {
	if strings.TrimSpace(modelName) == "seedance-gateway" {
		return true
	}
	if info == nil {
		return false
	}
	return strings.TrimSpace(info.OriginModelName) == "seedance-gateway" ||
		strings.TrimSpace(info.UpstreamModelName) == "seedance-gateway"
}

func mustParsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func maxVideoSecondsForModel(info *relaycommon.RelayInfo) int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	if strings.TrimSpace(info.UpstreamModelName) == canvasStandardSeedanceModel {
		return canvasStandardMaxVideoSeconds
	}
	return 0
}

func clampVideoSeconds(seconds, maxSeconds int) int {
	if maxSeconds > 0 && seconds > maxSeconds {
		return maxSeconds
	}
	return seconds
}

func clampNormalizedVideoSeconds(seconds string, maxSeconds int) string {
	return strconv.Itoa(clampVideoSeconds(mustParsePositiveInt(seconds), maxSeconds))
}

func clampRawVideoSeconds(value string, maxSeconds int) string {
	if maxSeconds <= 0 {
		return value
	}
	seconds, ok := normalizeVideoSeconds(value)
	if !ok {
		return value
	}
	return clampNormalizedVideoSeconds(seconds, maxSeconds)
}

func clampJSONVideoDuration(body map[string]interface{}, maxSeconds int) {
	if maxSeconds <= 0 {
		return
	}
	seconds, ok := normalizeVideoSeconds(body["duration"])
	if !ok {
		return
	}
	body["duration"] = mustParsePositiveInt(clampNormalizedVideoSeconds(seconds, maxSeconds))
}

func normalizeVideoSecondsFromForm(values map[string][]string) string {
	if len(values) == 0 {
		return ""
	}
	if rawValues := values["seconds"]; len(rawValues) > 0 {
		if seconds, ok := normalizeVideoSeconds(rawValues[0]); ok {
			return seconds
		}
	}
	if rawValues := values["duration"]; len(rawValues) > 0 {
		if seconds, ok := normalizeVideoSeconds(rawValues[0]); ok {
			return seconds
		}
	}
	return ""
}

func normalizeVideoSeconds(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		seconds := strings.TrimSpace(strings.ToLower(v))
		seconds = strings.TrimSuffix(seconds, "seconds")
		seconds = strings.TrimSuffix(seconds, "second")
		seconds = strings.TrimSuffix(seconds, "secs")
		seconds = strings.TrimSuffix(seconds, "sec")
		seconds = strings.TrimSuffix(seconds, "s")
		seconds = strings.TrimSpace(seconds)
		if seconds == "" {
			return "", false
		}
		if f, err := strconv.ParseFloat(seconds, 64); err == nil && f > 0 {
			return strconv.Itoa(int(f)), true
		}
		return "", false
	case int:
		if v > 0 {
			return strconv.Itoa(v), true
		}
	case int64:
		if v > 0 {
			return strconv.FormatInt(v, 10), true
		}
	case float64:
		if v > 0 {
			return strconv.Itoa(int(v)), true
		}
	case float32:
		if v > 0 {
			return strconv.Itoa(int(v)), true
		}
	}
	return "", false
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

	// Parse Sora response
	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamID := dResp.ID
	if upstreamID == "" {
		upstreamID = dResp.TaskID
	}
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用公开 task_xxxx ID 返回给客户端
	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	c.JSON(http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func responseTaskFailureReason(task responseTask) string {
	if task.Error != nil && strings.TrimSpace(task.Error.Message) != "" {
		return strings.TrimSpace(task.Error.Message)
	}
	if strings.TrimSpace(task.Message) != "" {
		return strings.TrimSpace(task.Message)
	}
	if reason := detailFailureReason(task.Detail); reason != "" {
		return reason
	}
	return ""
}

func detailFailureReason(detail any) string {
	switch v := detail.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		if msg, _ := v["message"].(string); strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg)
		}
		if typ, _ := v["type"].(string); strings.Contains(strings.ToLower(typ), "error") {
			return strings.TrimSpace(typ)
		}
	}
	return ""
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	switch strings.ToLower(strings.TrimSpace(resTask.Status)) {
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress", "running":
		taskResult.Status = model.TaskStatusInProgress
	case "completed", "complete", "done", "succeeded", "success":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = extractResponseTaskVideoURL(resTask)
	case "failed", "cancelled", "canceled", "error":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = responseTaskFailureReason(resTask)
	default:
		if reason := responseTaskFailureReason(resTask); reason != "" {
			taskResult.Status = model.TaskStatusFailure
			taskResult.Reason = reason
		}
	}
	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	payload := map[string]any{}
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &payload); err != nil {
			return nil, errors.Wrap(err, "unmarshal sora video response failed")
		}
	}

	payload["id"] = task.TaskID
	if strings.TrimSpace(stringValue(payload["object"])) == "" {
		payload["object"] = "video"
	}
	if upstreamTaskID := strings.TrimSpace(task.GetUpstreamTaskID()); upstreamTaskID != "" && upstreamTaskID != task.TaskID {
		payload["task_id"] = upstreamTaskID
	}
	if strings.TrimSpace(stringValue(payload["model"])) == "" && task.Properties.OriginModelName != "" {
		payload["model"] = task.Properties.OriginModelName
	}
	payload["status"] = toSoraCompatibleVideoStatus(task.Status, stringValue(payload["status"]))
	if _, ok := payload["progress"]; !ok {
		progress, _ := strconv.Atoi(strings.TrimSuffix(task.Progress, "%"))
		payload["progress"] = progress
	}
	if _, ok := payload["created_at"]; !ok && task.CreatedAt > 0 {
		payload["created_at"] = task.CreatedAt
	}
	if _, ok := payload["completed_at"]; !ok && task.FinishTime > 0 {
		payload["completed_at"] = task.FinishTime
	}

	if firstNonEmpty(extractVideoURLFromAny(payload), task.GetResultURL()) != "" {
		// Sora-compatible upstreams may return protected content URLs that only work
		// with the upstream API key. Never expose those directly to clients; return
		// our authenticated content proxy while keeping the stored task data
		// untouched for the proxy fetch path.
		url := taskcommon.BuildProxyURL(task.TaskID)
		setSoraResponseVideoURL(payload, url)
	}

	return common.Marshal(payload)
}

func setSoraResponseVideoURL(payload map[string]any, url string) {
	if payload == nil || strings.TrimSpace(url) == "" {
		return
	}
	payload["result_url"] = url
	payload["url"] = url
	payload["video_url"] = url
	payload["output"] = []string{url}

	video, _ := payload["video"].(map[string]any)
	if video == nil {
		video = map[string]any{}
	}
	video["url"] = url
	payload["video"] = video

	for _, key := range []string{"metadata", "response", "data"} {
		child, _ := payload[key].(map[string]any)
		if child == nil {
			continue
		}
		child["result_url"] = url
		child["url"] = url
		child["video_url"] = url
		child["result_urls"] = []string{url}
	}
}

func toSoraCompatibleVideoStatus(status model.TaskStatus, raw string) string {
	if converted := status.ToVideoStatus(); converted != dto.VideoStatusUnknown {
		return converted
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "completed", "complete", "done", "succeeded", "success":
		return dto.VideoStatusCompleted
	case "processing", "in_progress", "running":
		return dto.VideoStatusInProgress
	case "pending", "queued", "submitted":
		return dto.VideoStatusQueued
	case "failed", "error", "cancelled", "canceled":
		return dto.VideoStatusFailed
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func extractVideoURLFromAny(v any) string {
	switch typed := v.(type) {
	case map[string]any:
		for _, key := range []string{"video_url", "result_url", "url", "uri"} {
			if url := strings.TrimSpace(stringValue(typed[key])); url != "" {
				return url
			}
		}
		for _, key := range []string{"output", "result_urls"} {
			if url := firstStringFromAnySlice(typed[key]); url != "" {
				return url
			}
		}
		for _, key := range []string{"video", "metadata", "response", "data"} {
			if url := extractVideoURLFromAny(typed[key]); url != "" {
				return url
			}
		}
	case []any:
		for _, item := range typed {
			if url := strings.TrimSpace(stringValue(item)); url != "" {
				return url
			}
		}
	}
	return ""
}

func firstStringFromAnySlice(v any) string {
	switch typed := v.(type) {
	case []any:
		for _, item := range typed {
			if url := strings.TrimSpace(stringValue(item)); url != "" {
				return url
			}
		}
	case []string:
		for _, item := range typed {
			if url := strings.TrimSpace(item); url != "" {
				return url
			}
		}
	}
	return ""
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
