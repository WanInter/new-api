package sora

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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

// ============================
// Request / Response structures
// ============================

type responseTask struct {
	ID                 string          `json:"id"`
	TaskID             string          `json:"task_id,omitempty"` //兼容旧接口
	CamelTaskID        string          `json:"taskId,omitempty"`
	Object             string          `json:"object"`
	Model              string          `json:"model"`
	Status             string          `json:"status"`
	Progress           int             `json:"progress"`
	CreatedAt          int64           `json:"created_at"`
	CompletedAt        int64           `json:"completed_at,omitempty"`
	ExpiresAt          int64           `json:"expires_at,omitempty"`
	Seconds            json.RawMessage `json:"seconds,omitempty"`
	Size               string          `json:"size,omitempty"`
	RemixedFromVideoID string          `json:"remixed_from_video_id,omitempty"`
	ResultURL          string          `json:"result_url,omitempty"`
	VideoURL           string          `json:"video_url,omitempty"`
	CamelVideoURL      string          `json:"videoUrl,omitempty"`
	OutputURL          string          `json:"output_url,omitempty"`
	URL                string          `json:"url,omitempty"`
	Output             any             `json:"output,omitempty"`
	Video              *struct {
		URL string `json:"url,omitempty"`
	} `json:"video,omitempty"`
	Error   any    `json:"error,omitempty"`
	Detail  any    `json:"detail,omitempty"`
	Message string `json:"message,omitempty"`
}

func extractResponseTaskVideoURL(task responseTask) string {
	for _, candidate := range []string{task.VideoURL, task.CamelVideoURL, task.ResultURL, task.OutputURL, task.URL} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	if task.Video != nil && strings.TrimSpace(task.Video.URL) != "" {
		return strings.TrimSpace(task.Video.URL)
	}
	if object := strings.TrimSpace(task.Object); isHTTPVideoURL(object) {
		return object
	}
	return extractVideoURLFromAny(task.Output)
}

func isHTTPVideoURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
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
	if _, err := relaycommon.NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
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

// ValidateMappedRequest applies strict Grok wire-contract checks after model
// mapping and before pricing. The two Grok models use images_url rather than
// the public image aliases, so unsupported references must not be discarded.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return nil
	}
	profile, hasProfile := soraModelProfileForInfo(info)
	if info.Action == constant.TaskActionRemix {
		if hasProfile && isGrokVideoProfile(profile) {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("Grok video models do not support remix requests"),
				"unsupported_action",
				http.StatusBadRequest,
			)
		}
		return nil
	}
	if !hasProfile || !isGrokVideoProfile(profile) {
		return nil
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if !strings.HasPrefix(contentType, "application/json") && !strings.Contains(contentType, "multipart/form-data") {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("Grok video models require application/json or multipart/form-data requests"),
			"invalid_request",
			http.StatusBadRequest,
		)
	}

	if err := validateGrokMultipartFiles(c); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_media_input", http.StatusBadRequest)
	}

	var body map[string]interface{}
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	input, err := grokRequestInputFromBody(body)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if len(input.Images) > 0 && info.Action == constant.TaskActionTextGenerate {
		info.Action = constant.TaskActionGenerate
	}

	switch profile.JSONTransform {
	case requestTransformGrokImageVideo:
		_, err = validateGrokImageVideoRequest(input)
	case requestTransformGrokVideo15:
		_, err = validateGrokVideo15Request(input)
	}
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func isGrokVideoProfile(profile soraModelProfile) bool {
	return profile.JSONTransform == requestTransformGrokImageVideo ||
		profile.JSONTransform == requestTransformGrokVideo15
}

// NormalizeBillingRequestBody gives request-aware pricing the same effective
// duration that the Grok payload will carry. The relay freezes this body before
// pre-consume, while BuildRequestBody runs after it.
func (a *TaskAdaptor) NormalizeBillingRequestBody(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	profile, hasProfile := soraModelProfileForInfo(info)
	if len(body) == 0 || !hasProfile || !isGrokVideoProfile(profile) {
		return body, nil
	}

	var bodyMap map[string]interface{}
	if err := common.Unmarshal(body, &bodyMap); err != nil {
		return nil, err
	}
	input, err := grokRequestInputFromBody(bodyMap)
	if err != nil {
		return nil, err
	}
	if !input.HasDuration && profile.DefaultDuration > 0 {
		bodyMap["duration"] = profile.DefaultDuration
	}
	return common.Marshal(bodyMap)
}

func validateGrokMultipartFiles(c *gin.Context) error {
	if c == nil || !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return err
	}
	defer form.RemoveAll()
	for _, files := range form.File {
		if len(files) > 0 {
			return fmt.Errorf("Grok video models do not support binary multipart files; use image URLs or data URIs")
		}
	}
	return nil
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

	seconds := float64(estimateVideoSeconds(req, info))
	if profile, ok := soraModelProfileForInfo(info); ok && profile.FixedDuration > 0 {
		seconds = float64(profile.FixedDuration)
	} else if tokenStackSeconds, ok := tokenStackBillingSeconds(c, info); ok {
		seconds = float64(tokenStackSeconds)
	} else if requestSeconds, selected, billable := genericSoraRequestBillingSeconds(c, info); selected {
		if billable {
			seconds = requestSeconds
		} else {
			// The selected top-level alias is forwarded verbatim. Do not price a
			// lower-priority seconds or metadata value when that wire value is
			// malformed; the upstream remains responsible for rejecting it.
			seconds = float64(defaultSoraVideoSeconds(info))
		}
	}
	size := req.Size
	if size == "" {
		size = "720x1280"
	}

	ratios := map[string]float64{
		"seconds": seconds,
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
	contentType := strings.ToLower(c.GetHeader("Content-Type"))

	if strings.HasPrefix(contentType, "application/json") {
		var bodyMap map[string]interface{}
		if err := common.Unmarshal(cachedBody, &bodyMap); err == nil {
			if err := applySoraJSONWireTransforms(c, info, bodyMap); err != nil {
				return nil, err
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
		if err := applyNormalizedSoraMultipartVideoOutput(c, formData.Value); err != nil {
			return nil, err
		}
		profile, hasProfile := soraModelProfileForInfo(info)
		if hasProfile && profile.MultipartTransform != requestTransformNone {
			if err := applySoraModelMultipartProfile(writer, formData.Value, profile); err != nil {
				return nil, err
			}
		} else {
			writeSoraMultipartFields(writer, formData.Value)
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

// BuildRequestBody intentionally starts from the original wire body because
// Sora-compatible providers have model-specific transforms. Once validation
// has produced a TaskSubmitReq, copy its normalized public output fields back
// into that body so routing, billing, and the provider see the same request.
func applyNormalizedSoraJSONVideoOutput(c *gin.Context, body map[string]interface{}) {
	if body == nil {
		return
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.AspectRatio != "" {
		body["aspect_ratio"] = req.AspectRatio
		if _, ok := body["ratio"]; ok {
			body["ratio"] = req.AspectRatio
		}
		if _, ok := body["aspectRatio"]; ok {
			body["aspectRatio"] = req.AspectRatio
		}
	}
	if req.Resolution != "" {
		body["resolution"] = req.Resolution
		setSoraParametersResolution(body, req.Resolution)
	}

	metadata, encodedMetadata := soraJSONMetadata(body)
	if metadata == nil {
		return
	}
	delete(metadata, "ratio")
	delete(metadata, "aspectRatio")
	delete(metadata, "aspect_ratio")
	if req.Size != "" {
		metadata["size"] = req.Size
	}
	if req.AspectRatio != "" {
		metadata["aspect_ratio"] = req.AspectRatio
	}
	if req.Resolution != "" {
		metadata["resolution"] = req.Resolution
	}
	if !encodedMetadata {
		return
	}
	if serialized, err := common.Marshal(metadata); err == nil {
		body["metadata"] = string(serialized)
	}
}

func soraJSONMetadata(body map[string]interface{}) (map[string]interface{}, bool) {
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		return metadata, false
	}
	rawMetadata, ok := body["metadata"].(string)
	if !ok || strings.TrimSpace(rawMetadata) == "" {
		return nil, false
	}
	var metadata map[string]interface{}
	if err := common.UnmarshalJsonStr(rawMetadata, &metadata); err != nil {
		return nil, false
	}
	return metadata, true
}

// setSoraParametersResolution keeps Sora's nested wire hint aligned with a
// canonical public resolution when callers send both forms. Do not create a
// parameters object for requests that did not use that provider-specific form.
func setSoraParametersResolution(body map[string]interface{}, resolution string) {
	parameters, ok := body["parameters"].(map[string]interface{})
	if !ok {
		return
	}
	parameters["resolution"] = resolution
}

func applyNormalizedSoraMultipartVideoOutput(c *gin.Context, values map[string][]string) error {
	if values == nil {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return relaycommon.ApplyNormalizedTaskMultipartVideoOutput(values, req, relaycommon.MultipartVideoOutputOptions{
		PreserveAspectRatioAliases: true,
	})
}

// mapDurationToSoraSeconds maps the public duration field to the Sora wire
// field without validating, rounding, or defaulting its value. duration is the
// canonical public spelling, so it takes precedence over the legacy seconds
// alias when both are present.
func mapDurationToSoraSeconds(body map[string]interface{}) {
	if body == nil {
		return
	}

	value, exists := soraDurationValueFromMap(body)
	if !exists {
		return
	}
	if seconds, ok := soraSecondsString(value); ok {
		body["seconds"] = seconds
	} else {
		body["seconds"] = value
	}
	delete(body, "duration")
}

// soraDurationValueFromMap mirrors the public duration alias precedence used
// by the generic Sora wire conversion. The presence of duration is
// significant even when its value is malformed: it is still the value that
// will be forwarded to the upstream instead of seconds.
func soraDurationValueFromMap(body map[string]interface{}) (interface{}, bool) {
	value, exists := body["duration"]
	if exists {
		return value, true
	}
	value, exists = body["seconds"]
	return value, exists
}

func soraSecondsString(value interface{}) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case int:
		return strconv.Itoa(value), true
	case int8:
		return strconv.FormatInt(int64(value), 10), true
	case int16:
		return strconv.FormatInt(int64(value), 10), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint:
		return strconv.FormatUint(uint64(value), 10), true
	case uint8:
		return strconv.FormatUint(uint64(value), 10), true
	case uint16:
		return strconv.FormatUint(uint64(value), 10), true
	case uint32:
		return strconv.FormatUint(uint64(value), 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	default:
		return "", false
	}
}

func writeSoraMultipartFields(writer *multipart.Writer, values map[string][]string) {
	for key, fieldValues := range values {
		if key == "model" || key == "duration" || key == "seconds" {
			continue
		}
		for _, value := range fieldValues {
			_ = writer.WriteField(key, value)
		}
	}

	seconds, _ := soraDurationValuesFromForm(values)
	for _, value := range seconds {
		_ = writer.WriteField("seconds", value)
	}
}

func soraDurationValuesFromForm(values map[string][]string) ([]string, bool) {
	duration, exists := values["duration"]
	if exists {
		return duration, true
	}
	seconds, exists := values["seconds"]
	return seconds, exists
}

func estimateVideoSeconds(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) int {
	if profile, ok := soraModelProfileForInfo(info); ok && profile.FixedDuration > 0 {
		return profile.FixedDuration
	}
	seconds := 0
	if req.Duration > 0 {
		seconds = req.Duration
	} else if normalized, ok := normalizeVideoSeconds(req.Seconds); ok {
		seconds = mustParsePositiveInt(normalized)
	} else if normalized, ok := normalizeVideoSeconds(req.Metadata["duration"]); ok {
		seconds = mustParsePositiveInt(normalized)
	} else {
		seconds = defaultSoraVideoSeconds(info)
	}
	return seconds
}

func defaultSoraVideoSeconds(info *relaycommon.RelayInfo) int {
	if profile, ok := soraModelProfileForInfo(info); ok && profile.DefaultDuration > 0 {
		return profile.DefaultDuration
	}
	return defaultUnprofiledVideoSeconds
}

// genericSoraRequestBillingSeconds reads the same top-level duration alias
// that mapDurationToSoraSeconds will send upstream. TaskSubmitReq stores
// duration as an int for legacy adaptors, so using it here would silently
// truncate a valid fractional duration before pricing. The final bool reports
// whether the selected raw wire value can be priced.
func genericSoraRequestBillingSeconds(c *gin.Context, info *relaycommon.RelayInfo) (seconds float64, selected bool, billable bool) {
	if profile, hasProfile := soraModelProfileForInfo(info); hasProfile && profile.SkipGenericDurationMapping {
		return 0, false, false
	}

	value, selected := genericSoraRequestDurationValue(c)
	if !selected {
		return 0, false, false
	}
	seconds, billable = parsePositiveSoraBillingSeconds(value)
	return seconds, true, billable
}

func genericSoraRequestDurationValue(c *gin.Context) (interface{}, bool) {
	if c == nil || c.Request == nil {
		return nil, false
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		body := make(map[string]interface{})
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			return nil, false
		}
		return soraDurationValueFromMap(body)
	}
	if !strings.Contains(contentType, "multipart/form-data") {
		return nil, false
	}

	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, false
	}
	defer form.RemoveAll()
	values, selected := soraDurationValuesFromForm(form.Value)
	if !selected {
		return nil, false
	}
	if len(values) != 1 {
		return nil, true
	}
	return values[0], true
}

func parsePositiveSoraBillingSeconds(value interface{}) (float64, bool) {
	var seconds float64
	switch typed := value.(type) {
	case string:
		value := strings.TrimSpace(strings.ToLower(typed))
		for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
			if strings.HasSuffix(value, suffix) {
				value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
				break
			}
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, false
		}
		seconds = parsed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		seconds = parsed
	case int:
		seconds = float64(typed)
	case int8:
		seconds = float64(typed)
	case int16:
		seconds = float64(typed)
	case int32:
		seconds = float64(typed)
	case int64:
		seconds = float64(typed)
	case uint:
		seconds = float64(typed)
	case uint8:
		seconds = float64(typed)
	case uint16:
		seconds = float64(typed)
	case uint32:
		seconds = float64(typed)
	case uint64:
		seconds = float64(typed)
	case float32:
		seconds = float64(typed)
	case float64:
		seconds = typed
	default:
		return 0, false
	}
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	return seconds, true
}

func tokenStackBillingSeconds(c *gin.Context, info *relaycommon.RelayInfo) (int, bool) {
	if !isTokenStackChannel(info) {
		return 0, false
	}
	upstreamModel := strings.TrimSpace(info.ChannelMeta.UpstreamModelName)
	if upstreamModel != tokenStackMultiModeModel {
		return 0, false
	}

	var request struct {
		Parameters struct {
			Duration any `json:"duration"`
		} `json:"parameters"`
	}
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return 0, false
	}
	seconds, ok := normalizeVideoSeconds(request.Parameters.Duration)
	if !ok {
		return 0, false
	}
	return mustParsePositiveInt(seconds), true
}

func mustParsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
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
		upstreamID = dResp.CamelTaskID
	}
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用公开 task_xxxx ID 返回给客户端
	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	dResp.CamelTaskID = info.PublicTaskID
	if strings.TrimSpace(info.OriginModelName) != "" {
		dResp.Model = info.OriginModelName
	}
	c.JSON(http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)

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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func responseTaskFailureReason(task responseTask) string {
	if reason := responseTaskErrorMessage(task.Error); reason != "" {
		return reason
	}
	if strings.TrimSpace(task.Message) != "" {
		return strings.TrimSpace(task.Message)
	}
	if reason := detailFailureReason(task.Detail); reason != "" {
		return reason
	}
	return ""
}

func responseTaskErrorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"message", "error", "code"} {
			if message, ok := typed[key].(string); ok && strings.TrimSpace(message) != "" {
				return strings.TrimSpace(message)
			}
		}
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

	status := strings.TrimSpace(resTask.Status)
	if statusReason, failed := taskStatusFailureReason(status); failed {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = firstNonEmpty(statusReason, responseTaskFailureReason(resTask))
	} else {
		switch strings.ToLower(status) {
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
			} else {
				return nil, fmt.Errorf("unknown Sora task status %q", resTask.Status)
			}
		}
	}
	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func taskStatusFailureReason(status string) (string, bool) {
	status = strings.TrimSpace(status)
	if len(status) < len("failed") || !strings.EqualFold(status[:len("failed")], "failed") {
		return "", false
	}
	reason := strings.TrimSpace(status[len("failed"):])
	reason = strings.TrimLeft(reason, ":：- ")
	return strings.TrimSpace(reason), true
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
	if task.Properties.OriginModelName != "" {
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
