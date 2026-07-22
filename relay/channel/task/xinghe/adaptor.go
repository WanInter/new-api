package xinghe

import (
	"bytes"
	"context"
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

type scalarOrStringList []string

func (values *scalarOrStringList) UnmarshalJSON(data []byte) error {
	var list []string
	if err := common.Unmarshal(data, &list); err == nil {
		*values = list
		return nil
	}
	var scalar string
	if err := common.Unmarshal(data, &scalar); err != nil {
		return err
	}
	*values = []string{scalar}
	return nil
}

type jsonRequest struct {
	Prompt                  string                        `json:"prompt"`
	Model                   string                        `json:"model,omitempty"`
	Image                   string                        `json:"image,omitempty"`
	Images                  scalarOrStringList            `json:"images,omitempty"`
	Content                 []relaycommon.TaskContentItem `json:"content,omitempty"`
	Size                    string                        `json:"size,omitempty"`
	Duration                int                           `json:"duration,omitempty"`
	Seconds                 string                        `json:"seconds,omitempty"`
	Ratio                   string                        `json:"ratio,omitempty"`
	AspectRatio             string                        `json:"aspect_ratio,omitempty"`
	AspectRatioCamel        string                        `json:"aspectRatio,omitempty"`
	Resolution              string                        `json:"resolution,omitempty"`
	ImageURLs               []string                      `json:"image_urls,omitempty"`
	Videos                  scalarOrStringList            `json:"videos,omitempty"`
	VideoURL                string                        `json:"video_url,omitempty"`
	VideoURLs               []string                      `json:"video_urls,omitempty"`
	VideosURLs              []string                      `json:"videos_urls,omitempty"`
	Audios                  scalarOrStringList            `json:"audios,omitempty"`
	AudioURL                string                        `json:"audio_url,omitempty"`
	AudioURLs               []string                      `json:"audio_urls,omitempty"`
	AudiosURLs              []string                      `json:"audios_urls,omitempty"`
	VoiceReferenceAudioURLs []string                      `json:"voice_reference_audio_urls,omitempty"`
	AudioReferenceURLs      []string                      `json:"audio_reference_urls,omitempty"`
	ReferenceVideoURL       string                        `json:"reference_video_url,omitempty"`
	ReferenceVideoURLs      []string                      `json:"reference_video_urls,omitempty"`
	ImitationVideoURLs      []string                      `json:"imitation_video_urls,omitempty"`
	SourceVideoURLs         []string                      `json:"source_video_urls,omitempty"`
	ReferenceAssetURLs      []string                      `json:"reference_asset_urls,omitempty"`
	Metadata                map[string]any                `json:"metadata,omitempty"`
}

type requestPayload struct {
	Model              string   `json:"model,omitempty"`
	Prompt             string   `json:"prompt"`
	Duration           int      `json:"duration,omitempty"`
	Ratio              string   `json:"ratio,omitempty"`
	Resolution         string   `json:"resolution,omitempty"`
	ImageURLs          []string `json:"image_urls,omitempty"`
	VideoURLs          []string `json:"video_urls,omitempty"`
	AudioURLs          []string `json:"audio_urls,omitempty"`
	ReferenceVideoURLs []string `json:"reference_video_urls,omitempty"`
	ClientTaskID       string   `json:"client_task_id,omitempty"`
}

type taskResponse struct {
	Status          string           `json:"status"`
	TaskID          string           `json:"task_id"`
	ID              string           `json:"id"`
	ClientTaskID    string           `json:"client_task_id"`
	Model           string           `json:"model"`
	TaskStatus      string           `json:"task_status"`
	Progress        any              `json:"progress"`
	VideoURL        string           `json:"video_url"`
	StableVideoURL  string           `json:"stable_video_url"`
	ResultURL       string           `json:"result_url"`
	URL             string           `json:"url"`
	Metadata        responseMetadata `json:"metadata"`
	Data            responseData     `json:"data"`
	QueryURL        string           `json:"query_url"`
	RequiredCredits int              `json:"required_credits"`
	BillingStatus   string           `json:"billing_status"`
	Message         string           `json:"message"`
	Error           responseError    `json:"error"`
}

type responseMetadata struct {
	ResultURLs []string `json:"result_urls"`
	VideoURL   string   `json:"video_url"`
	URL        string   `json:"url"`
}

type responseData struct {
	VideoURL       string           `json:"video_url"`
	StableVideoURL string           `json:"stable_video_url"`
	ResultURL      string           `json:"result_url"`
	URL            string           `json:"url"`
	Metadata       responseMetadata `json:"metadata"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var raw jsonRequest
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	req := relaycommon.TaskSubmitReq{
		Prompt:           raw.Prompt,
		Model:            raw.Model,
		Images:           []string(raw.Images),
		Content:          raw.Content,
		Videos:           []string(raw.Videos),
		Audios:           []string(raw.Audios),
		Size:             raw.Size,
		Ratio:            raw.Ratio,
		AspectRatio:      raw.AspectRatio,
		AspectRatioAlias: raw.AspectRatioCamel,
		Resolution:       raw.Resolution,
		Duration:         raw.Duration,
		Seconds:          raw.Seconds,
		Metadata:         raw.Metadata,
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
	if len(req.Images) == 0 && raw.Image != "" {
		req.Images = []string{raw.Image}
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	copyRawMetadata(raw, req.Metadata)
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	c.Set("task_request", req)
	info.Action = constant.TaskActionGenerate
	return nil
}

func copyRawMetadata(raw jsonRequest, metadata map[string]any) {
	setString := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			metadata[key] = value
		}
	}
	setString("ratio", raw.Ratio)
	setString("aspect_ratio", raw.AspectRatio)
	setString("aspectRatio", raw.AspectRatioCamel)
	setString("resolution", raw.Resolution)
	appendList := func(key string, values ...string) {
		merged := append(stringList(metadata[key]), values...)
		if len(merged) > 0 {
			metadata[key] = merged
		}
	}
	appendSlice := func(key string, values []string) {
		appendList(key, values...)
	}
	appendSlice("image_urls", raw.ImageURLs)
	appendList("video_urls", raw.VideoURL)
	appendSlice("video_urls", raw.VideoURLs)
	appendSlice("video_urls", raw.VideosURLs)
	appendList("audio_urls", raw.AudioURL)
	appendSlice("audio_urls", raw.AudioURLs)
	appendSlice("audio_urls", raw.AudiosURLs)
	appendSlice("audio_urls", raw.VoiceReferenceAudioURLs)
	appendSlice("audio_urls", raw.AudioReferenceURLs)
	appendList("reference_video_urls", raw.ReferenceVideoURL)
	appendSlice("reference_video_urls", raw.ReferenceVideoURLs)
	appendSlice("reference_video_urls", raw.ImitationVideoURLs)
	appendSlice("reference_video_urls", raw.SourceVideoURLs)
	appendSlice("reference_asset_urls", raw.ReferenceAssetURLs)
	if raw.Seconds != "" && raw.Duration == 0 {
		if v, err := strconv.Atoi(strings.TrimSpace(raw.Seconds)); err == nil && v > 0 {
			metadata["duration"] = v
		}
	}
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
	return map[string]float64{"seconds": float64(payload.Duration), "resolution": resolutionRatio(payload.Model, payload.Resolution)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/api/generate-video", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// ValidateMappedRequest runs after model mapping, before pricing. Xinghe has
// fixed media limits and model-specific output options, so silently clipping
// inputs or falling back to defaults would bill for a request different from
// the one the client submitted.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	modelName := upstreamModelName(info)
	if modelName == "" {
		modelName = req.Model
	}
	if _, err := resolveXingheRatio(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	if _, err := resolveXingheResolution(&req, modelName); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	if err := validateXingheMediaInputs(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_media_input", http.StatusBadRequest)
	}
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
	var parsed taskResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	taskID := firstNonEmpty(parsed.TaskID, parsed.ID)
	if strings.TrimSpace(taskID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("Xinghe create task returned empty task_id: %s", errorMessage(parsed)), "submit_failed", http.StatusBadRequest)
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = mapStatus(firstNonEmpty(parsed.TaskStatus, parsed.Status))
	c.JSON(http.StatusOK, ov)
	return taskID, body, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := strings.TrimRight(baseUrl, "/") + "/api/task/" + url.PathEscape(taskID)
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
	var parsed taskResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	info := &relaycommon.TaskInfo{Code: 0}
	status := strings.ToLower(strings.TrimSpace(firstNonEmpty(parsed.TaskStatus, parsed.Status)))
	switch status {
	case "success", "succeeded", "completed":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
		info.Url = resultURL(parsed)
	case "failed", "fail", "error", "cancelled", "canceled":
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		info.Reason = errorMessage(parsed)
	case "submitted", "queued", "pending", "running", "processing", "in_progress":
		info.Status = model.TaskStatusInProgress
		info.Progress = progressString(parsed.Progress, "30%")
	default:
		return nil, fmt.Errorf("unknown Xinghe task status %q", status)
	}
	return info, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	ov := originTask.ToOpenAIVideo()
	var parsed taskResponse
	if len(originTask.Data) > 0 {
		_ = common.Unmarshal(originTask.Data, &parsed)
	}
	if u := resultURL(parsed); u != "" {
		ov.SetMetadata("url", u)
		ov.SetMetadata("video_url", u)
		ov.SetMetadata("result_url", u)
	}
	if parsed.Message != "" || parsed.Error.Message != "" || originTask.Status == model.TaskStatusFailure {
		ov.Error = &dto.OpenAIVideoError{Message: firstNonEmpty(parsed.Error.Message, parsed.Error.Code, parsed.Message, originTask.FailReason), Code: parsed.Error.Code}
	}
	return common.Marshal(ov)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	modelName := upstreamModelName(info)
	if modelName == "" {
		modelName = req.Model
	}
	if req.Model != "" && !isModelMapped(info) {
		modelName = req.Model
	}
	ratio, err := resolveXingheRatio(req)
	if err != nil {
		return nil, err
	}
	resolution, err := resolveXingheResolution(req, modelName)
	if err != nil {
		return nil, err
	}
	if err := validateXingheMediaInputs(req); err != nil {
		return nil, err
	}
	images, videos, audios := xingheMediaURLs(req)
	referenceVideos := xingheReferenceVideoURLs(req)
	payload := &requestPayload{
		Model:        modelName,
		Prompt:       req.Prompt,
		Duration:     normalizeDuration(req.Duration, req.Seconds, req.Metadata),
		Ratio:        ratio,
		Resolution:   resolution,
		ImageURLs:    limitStrings(images, 9),
		VideoURLs:    limitStrings(videos, 3),
		AudioURLs:    limitStrings(audios, 3),
		ClientTaskID: publicTaskID(info),
	}
	payload.ReferenceVideoURLs = limitStrings(referenceVideos, 3)
	if len(payload.ImageURLs) == 0 && len(payload.VideoURLs) == 0 && len(payload.AudioURLs) == 0 && len(payload.ReferenceVideoURLs) == 0 {
		return nil, fmt.Errorf("Xinghe video requires at least one image, video, or audio reference asset")
	}
	return payload, nil
}

func upstreamModelName(info *relaycommon.RelayInfo) string {
	if info != nil && info.ChannelMeta != nil {
		return info.UpstreamModelName
	}
	return ""
}

func isModelMapped(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.IsModelMapped
}

func publicTaskID(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return info.PublicTaskID
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
	if duration < 4 {
		return 4
	}
	if duration > 15 {
		return 15
	}
	return duration
}

func resolveXingheRatio(req *relaycommon.TaskSubmitReq) (string, error) {
	if req == nil {
		return "", fmt.Errorf("video request is required")
	}
	ratio := strings.TrimSpace(firstNonEmpty(req.AspectRatio, req.Ratio, stringValue(req.Metadata["ratio"]), stringValue(req.Metadata["aspect_ratio"])))
	if ratio == "" {
		return DefaultRatio, nil
	}
	if ratio == DefaultRatio || ratio == "9:16" {
		return ratio, nil
	}
	return "", fmt.Errorf("Xinghe supports only 16:9 and 9:16 aspect ratios, got %q", ratio)
}

func resolveXingheResolution(req *relaycommon.TaskSubmitReq, modelName string) (string, error) {
	if req == nil {
		return "", fmt.Errorf("video request is required")
	}
	resolution := strings.TrimSpace(firstNonEmpty(req.Resolution, stringValue(req.Metadata["resolution"])))
	if resolution == "" || resolution == DefaultResolution {
		return DefaultResolution, nil
	}
	if resolution == "1080p" && strings.TrimSpace(modelName) == "xinghe-2.0" {
		return resolution, nil
	}
	return "", fmt.Errorf("Xinghe model %q does not support resolution %q", modelName, resolution)
}

func validateXingheMediaInputs(req *relaycommon.TaskSubmitReq) error {
	images, videos, audios := xingheMediaURLs(req)
	referenceVideos := xingheReferenceVideoURLs(req)
	for _, input := range []struct {
		name   string
		values []string
		limit  int
	}{
		{name: "image", values: images, limit: 9},
		{name: "video", values: videos, limit: 3},
		{name: "audio", values: audios, limit: 3},
		{name: "reference video", values: referenceVideos, limit: 3},
	} {
		if len(input.values) > input.limit {
			return fmt.Errorf("Xinghe supports at most %d %s inputs, got %d", input.limit, input.name, len(input.values))
		}
	}
	return nil
}

func xingheMediaURLs(req *relaycommon.TaskSubmitReq) (images, videos, audios []string) {
	if req == nil {
		return nil, nil, nil
	}
	contentImages, contentVideos, contentAudios := xingheContentMediaURLs(req.Content)
	return mergeStrings(req.Images, req.ImageURLs, stringList(req.Metadata["image_urls"]), contentImages),
		mergeStrings(req.Videos, req.VideoURLs, stringList(req.Metadata["video_urls"]), contentVideos),
		mergeStrings(req.Audios, req.AudioURLs, stringList(req.Metadata["audio_urls"]), contentAudios)
}

func xingheReferenceVideoURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	return mergeStrings(stringList(req.Metadata["reference_video_urls"]), stringList(req.Metadata["reference_asset_urls"]))
}

func xingheContentMediaURLs(content []relaycommon.TaskContentItem) (images, videos, audios []string) {
	for _, item := range content {
		if item.ImageURL != nil {
			images = append(images, item.ImageURL.URL)
		}
		if item.VideoURL != nil {
			videos = append(videos, item.VideoURL.URL)
		}
		if item.AudioURL != nil {
			audios = append(audios, item.AudioURL.URL)
		}
	}
	return mergeStrings(images), mergeStrings(videos), mergeStrings(audios)
}

func resolutionRatio(modelName, resolution string) float64 {
	if strings.TrimSpace(modelName) == "xinghe-2.0" && strings.TrimSpace(resolution) == "1080p" {
		return 2.1
	}
	return 1
}

func resultURL(r taskResponse) string {
	return firstNonEmpty(firstString(r.Metadata.ResultURLs), r.Metadata.VideoURL, r.Metadata.URL, firstString(r.Data.Metadata.ResultURLs), r.Data.Metadata.VideoURL, r.Data.Metadata.URL, r.StableVideoURL, r.Data.StableVideoURL, r.Data.VideoURL, r.Data.URL, r.Data.ResultURL, r.VideoURL, r.URL, r.ResultURL)
}

func errorMessage(r taskResponse) string {
	return firstNonEmpty(r.Error.Message, r.Error.Code, r.Message, "unknown error")
}

func mapStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "completed":
		return dto.VideoStatusCompleted
	case "failed", "fail", "error", "cancelled", "canceled":
		return dto.VideoStatusFailed
	case "queued", "pending":
		return dto.VideoStatusQueued
	default:
		return dto.VideoStatusInProgress
	}
}

func progressString(v any, fallback string) string {
	switch t := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f%%", t)
	case int:
		return fmt.Sprintf("%d%%", t)
	case string:
		if strings.TrimSpace(t) != "" {
			if strings.HasSuffix(t, "%") {
				return t
			}
			return t + "%"
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstString(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}
func limitStrings(values []string, limit int) []string {
	out := make([]string, 0)
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func mergeStrings(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, values := range groups {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			merged = append(merged, value)
		}
	}
	return merged
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func intValue(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}
func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s := stringValue(x); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.Contains(t, ",") {
			return strings.Split(t, ",")
		}
		if strings.TrimSpace(t) != "" {
			return []string{t}
		}
	}
	return nil
}
