package aggc

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

const (
	ChannelName     = "aggc"
	DefaultBaseURL  = "https://aggc.site"
	DefaultDuration = 4
)

var ModelList = []string{"seedance-2.0"}

type jsonRequest struct {
	Prompt           string         `json:"prompt"`
	Model            string         `json:"model,omitempty"`
	ModelID          string         `json:"model_id,omitempty"`
	Type             string         `json:"type,omitempty"`
	Image            string         `json:"image,omitempty"`
	Images           []string       `json:"images,omitempty"`
	ImageURLs        []string       `json:"image_urls,omitempty"`
	VideoURL         string         `json:"video_url,omitempty"`
	VideoURLs        []string       `json:"video_urls,omitempty"`
	AudioURL         string         `json:"audio_url,omitempty"`
	AudioURLs        []string       `json:"audio_urls,omitempty"`
	Duration         int            `json:"duration,omitempty"`
	Seconds          string         `json:"seconds,omitempty"`
	Size             string         `json:"size,omitempty"`
	Orientation      string         `json:"orientation,omitempty"`
	Watermark        *bool          `json:"watermark,omitempty"`
	AspectRatio      string         `json:"aspect_ratio,omitempty"`
	AspectRatioCamel string         `json:"aspectRatio,omitempty"`
	Ratio            string         `json:"ratio,omitempty"`
	Params           map[string]any `json:"params,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type requestPayload struct {
	ModelID string        `json:"model_id"`
	Type    string        `json:"type"`
	Prompt  string        `json:"prompt"`
	Params  requestParams `json:"params,omitempty"`
}

type requestParams struct {
	Duration    int      `json:"duration,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Orientation string   `json:"orientation,omitempty"`
	Watermark   *bool    `json:"watermark,omitempty"`
	AudioURLs   []string `json:"audioUrls,omitempty"`
	ImageURLs   []string `json:"imageUrls,omitempty"`
	VideoURLs   []string `json:"videoUrls,omitempty"`
	AspectRatio string   `json:"aspectRatio,omitempty"`
}

type submitResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    submitResponseData `json:"data"`
}

type submitResponseData struct {
	JobID  any    `json:"job_id"`
	ID     any    `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type queryResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    queryResponseData `json:"data"`
}

type queryResponseData struct {
	JobID         any    `json:"job_id"`
	Status        string `json:"status"`
	VideoURL      string `json:"video_url"`
	VideoCoverURL string `json:"video_cover_url"`
	Message       string `json:"message"`
	Error         string `json:"error"`
	FailReason    string `json:"fail_reason"`
	Progress      any    `json:"progress"`
}

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
	var raw jsonRequest
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	prompt := strings.TrimSpace(raw.Prompt)
	if prompt == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	modelName := firstNonEmpty(raw.Model, raw.ModelID, info.OriginModelName)
	if strings.TrimSpace(modelName) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	metadata := raw.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	copyAggcRawMetadata(raw, metadata)
	req := relaycommon.TaskSubmitReq{
		Prompt:   prompt,
		Model:    modelName,
		Images:   mergeStrings(raw.Images, singleString(raw.Image), raw.ImageURLs),
		Duration: normalizeDuration(raw.Duration, raw.Seconds, metadata),
		Seconds:  raw.Seconds,
		Metadata: metadata,
	}
	c.Set("task_request", req)
	info.Action = constant.TaskActionTextGenerate
	if len(req.Images) > 0 || len(stringList(metadata["video_urls"])) > 0 || len(stringList(metadata["audio_urls"])) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func copyAggcRawMetadata(raw jsonRequest, metadata map[string]any) {
	if strings.TrimSpace(raw.Type) != "" {
		metadata["type"] = raw.Type
	}
	if aspectRatio := firstNonEmpty(raw.AspectRatio, raw.AspectRatioCamel); aspectRatio != "" {
		metadata["aspect_ratio"] = aspectRatio
	}
	if strings.TrimSpace(raw.Ratio) != "" {
		metadata["ratio"] = raw.Ratio
	}
	if strings.TrimSpace(raw.Size) != "" {
		metadata["size"] = raw.Size
	}
	if strings.TrimSpace(raw.Orientation) != "" {
		metadata["orientation"] = raw.Orientation
	}
	if raw.Watermark != nil {
		metadata["watermark"] = *raw.Watermark
	}
	if len(raw.Params) > 0 {
		for k, v := range raw.Params {
			metadata[k] = v
		}
	}
	if strings.TrimSpace(raw.VideoURL) != "" {
		metadata["video_urls"] = mergeStrings(stringList(metadata["video_urls"]), []string{raw.VideoURL}, raw.VideoURLs)
	} else if len(raw.VideoURLs) > 0 {
		metadata["video_urls"] = mergeStrings(stringList(metadata["video_urls"]), raw.VideoURLs)
	}
	if strings.TrimSpace(raw.AudioURL) != "" {
		metadata["audio_urls"] = mergeStrings(stringList(metadata["audio_urls"]), []string{raw.AudioURL}, raw.AudioURLs)
	} else if len(raw.AudioURLs) > 0 {
		metadata["audio_urls"] = mergeStrings(stringList(metadata["audio_urls"]), raw.AudioURLs)
	}
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(normalizeDuration(req.Duration, req.Seconds, req.Metadata))}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/api/v1/prot/generate", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	payload, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, err
	}
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()
	var parsed submitResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if parsed.Code != 0 {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("AGGC submit failed: %s", parsed.Message), "submit_failed", http.StatusBadRequest)
	}
	taskID := firstNonEmpty(parsed.Data.TaskID, anyToString(parsed.Data.JobID), anyToString(parsed.Data.ID))
	if strings.TrimSpace(taskID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("AGGC create task returned empty job_id: %s", parsed.Message), "submit_failed", http.StatusBadRequest)
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = mapAggcStatus(parsed.Data.Status).ToVideoStatus()
	c.JSON(http.StatusOK, ov)
	return taskID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseUrl, "/") + "/api/v1/prot/query/" + url.PathEscape(taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var parsed queryResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	info := &relaycommon.TaskInfo{Code: parsed.Code}
	if parsed.Code != 0 {
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		info.Reason = firstNonEmpty(parsed.Message, "AGGC task query failed")
		return info, nil
	}
	info.TaskID = anyToString(parsed.Data.JobID)
	status := mapAggcStatus(parsed.Data.Status)
	info.Status = string(status)
	info.Progress = progressString(parsed.Data.Progress, status)
	if status == model.TaskStatusSuccess {
		info.Url = parsed.Data.VideoURL
		info.Progress = "100%"
	}
	if status == model.TaskStatusFailure {
		info.Progress = "100%"
		info.Reason = firstNonEmpty(parsed.Data.FailReason, parsed.Data.Error, parsed.Data.Message, parsed.Message, "task failed")
	}
	return info, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var parsed queryResponse
	if len(originTask.Data) > 0 {
		_ = common.Unmarshal(originTask.Data, &parsed)
	}

	out := map[string]any{
		"id":       originTask.TaskID,
		"object":   "video",
		"model":    originTask.Properties.OriginModelName,
		"progress": progressInt(originTask.Progress, parsed.Data.Progress),
		"status":   aggcOpenAIStatus(originTask.Status, parsed.Data.Status),
	}
	if upstreamTaskID := strings.TrimSpace(originTask.GetUpstreamTaskID()); upstreamTaskID != "" && upstreamTaskID != originTask.TaskID {
		out["task_id"] = upstreamTaskID
	}
	if originTask.CreatedAt > 0 {
		out["created_at"] = originTask.CreatedAt
	}
	if originTask.FinishTime > 0 {
		out["completed_at"] = originTask.FinishTime
	} else if originTask.UpdatedAt > 0 && originTask.Status == model.TaskStatusSuccess {
		out["completed_at"] = originTask.UpdatedAt
	}

	if u := firstNonEmpty(parsed.Data.VideoURL, originTask.GetResultURL()); u != "" {
		out["result_url"] = u
		out["url"] = u
		out["video_url"] = u
		out["output"] = []string{u}
		out["video"] = map[string]any{"url": u}
	}
	if parsed.Data.VideoCoverURL != "" {
		out["video_cover_url"] = parsed.Data.VideoCoverURL
	}
	if originTask.Status == model.TaskStatusFailure {
		out["error"] = &dto.OpenAIVideoError{Message: firstNonEmpty(parsed.Data.FailReason, parsed.Data.Error, parsed.Data.Message, originTask.FailReason), Code: fmt.Sprintf("%d", parsed.Code)}
	}
	return common.Marshal(out)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func aggcOpenAIStatus(status model.TaskStatus, raw string) string {
	if converted := status.ToVideoStatus(); converted != dto.VideoStatusUnknown {
		return converted
	}
	switch mapAggcStatus(raw) {
	case model.TaskStatusSuccess:
		return dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		return dto.VideoStatusFailed
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return dto.VideoStatusQueued
	default:
		return dto.VideoStatusInProgress
	}
}

func progressInt(progress string, fallback any) int {
	progress = strings.TrimSpace(strings.TrimSuffix(progress, "%"))
	if progress != "" {
		if n, err := strconv.Atoi(progress); err == nil {
			return n
		}
	}
	if s := strings.TrimSpace(anyToString(fallback)); s != "" {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "%")); err == nil {
			return n
		}
	}
	return 0
}
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	upstreamModelName := ""
	if info != nil && info.ChannelMeta != nil {
		upstreamModelName = info.ChannelMeta.UpstreamModelName
	}
	modelName := firstNonEmpty(upstreamModelName, req.Model)
	aspectRatio := normalizeAspectRatio(req.Metadata)
	if aspectRatio == "" {
		aspectRatio = aspectRatioFromSizeOrOrientation(normalizeSize(req.Size, req.Metadata), stringValue(req.Metadata["orientation"]))
	}
	params := requestParams{
		Duration:    normalizeDuration(req.Duration, req.Seconds, req.Metadata),
		Resolution:  normalizeResolution(req.Metadata, req.Size),
		Orientation: stringValue(req.Metadata["orientation"]),
		Watermark:   boolPointer(req.Metadata["watermark"]),
		ImageURLs:   mergeStrings(req.Images, stringList(req.Metadata["image_urls"])),
		VideoURLs:   stringList(req.Metadata["video_urls"]),
		AudioURLs:   stringList(req.Metadata["audio_urls"]),
		AspectRatio: aspectRatio,
	}
	payload := &requestPayload{
		ModelID: modelName,
		Type:    firstNonEmpty(stringValue(req.Metadata["type"]), "video"),
		Prompt:  req.Prompt,
		Params:  params,
	}
	return payload, nil
}

func mapAggcStatus(status string) model.TaskStatus {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "success", "succeeded", "completed", "complete", "done", "finish", "finished":
		return model.TaskStatusSuccess
	case "fail", "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	case "queued", "queue", "pending", "submitted", "created", "waiting":
		return model.TaskStatusQueued
	case "running", "processing", "in_progress", "progress":
		return model.TaskStatusInProgress
	default:
		return model.TaskStatusInProgress
	}
}

func progressString(value any, status model.TaskStatus) string {
	if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
		return "100%"
	}
	if value == nil {
		if status == model.TaskStatusQueued {
			return "20%"
		}
		return "30%"
	}
	if s := strings.TrimSpace(anyToString(value)); s != "" {
		if strings.HasSuffix(s, "%") {
			return s
		}
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return s + "%"
		}
	}
	return "30%"
}

func normalizeDuration(duration int, seconds string, metadata map[string]any) int {
	if duration <= 0 {
		duration = intValue(metadata["duration"])
	}
	if duration <= 0 && strings.TrimSpace(seconds) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(seconds)); err == nil {
			duration = v
		}
	}
	if duration <= 0 {
		duration = DefaultDuration
	}
	return duration
}

func normalizeAspectRatio(metadata map[string]any) string {
	return firstNonEmpty(stringValue(metadata["aspectRatio"]), stringValue(metadata["aspect_ratio"]), stringValue(metadata["ratio"]))
}

func normalizeSize(size string, metadata map[string]any) string {
	return firstNonEmpty(size, stringValue(metadata["size"]))
}

func normalizeResolution(metadata map[string]any, size string) string {
	resolution := firstNonEmpty(stringValue(metadata["resolution"]), stringValue(metadata["分辨率"]))
	if resolution != "" {
		return resolution
	}
	return resolutionFromSize(normalizeSize(size, metadata))
}

func resolutionFromSize(size string) string {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(size, "×", "x")))
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return ""
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ""
	}
	shortSide := w
	if h < shortSide {
		shortSide = h
	}
	return fmt.Sprintf("%dp", shortSide)
}

func aspectRatioFromSizeOrOrientation(size, orientation string) string {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(size, "×", "x")))
	parts := strings.Split(s, "x")
	if len(parts) == 2 {
		w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errW == nil && errH == nil && w > 0 && h > 0 {
			g := gcd(w, h)
			return fmt.Sprintf("%d:%d", w/g, h/g)
		}
	}
	switch strings.ToLower(strings.TrimSpace(orientation)) {
	case "landscape":
		return "16:9"
	case "portrait":
		return "9:16"
	case "square":
		return "1:1"
	default:
		return ""
	}
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	if a == 0 {
		return 1
	}
	return a
}

func boolPointer(value any) *bool {
	switch v := value.(type) {
	case bool:
		return &v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}

func mergeStrings(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, values := range groups {
		for _, value := range values {
			v := strings.TrimSpace(value)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func singleString(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func stringList(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(anyToString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func stringValue(value any) string {
	return strings.TrimSpace(anyToString(value))
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(v))
		return i
	default:
		return 0
	}
}

func anyToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprintf("%v", v)
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
