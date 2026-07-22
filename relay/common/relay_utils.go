package common

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
)

type HasPrompt interface {
	GetPrompt() string
}

type HasImage interface {
	HasImage() bool
}

func GetFullRequestURL(baseURL string, requestURL string, channelType int) string {
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		switch channelType {
		case constant.ChannelTypeOpenAI:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/v1"))
		case constant.ChannelTypeAzure:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
		}
	}
	return fullRequestURL
}

func GetAPIVersion(c *gin.Context) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion == "" {
		apiVersion = c.GetString("api_version")
	}
	return apiVersion
}

func createTaskError(err error, code string, statusCode int, localError bool) *dto.TaskError {
	return &dto.TaskError{
		Code:       code,
		Message:    err.Error(),
		StatusCode: statusCode,
		LocalError: localError,
		Error:      err,
	}
}

func storeTaskRequest(c *gin.Context, info *RelayInfo, action string, requestObj TaskSubmitReq) {
	info.Action = action
	c.Set("task_request", requestObj)
}
func GetTaskRequest(c *gin.Context) (TaskSubmitReq, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return TaskSubmitReq{}, fmt.Errorf("request not found in context")
	}
	req, ok := v.(TaskSubmitReq)
	if !ok {
		return TaskSubmitReq{}, fmt.Errorf("invalid task request type")
	}
	return req, nil
}

func validatePrompt(prompt string) *dto.TaskError {
	if strings.TrimSpace(prompt) == "" {
		return createTaskError(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest, true)
	}
	return nil
}

func validateMultipartTaskRequest(c *gin.Context, info *RelayInfo, action string) (TaskSubmitReq, error) {
	var req TaskSubmitReq
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return req, err
	}
	defer form.RemoveAll()

	formData := url.Values(form.Value)
	req = TaskSubmitReq{
		Prompt:           formData.Get("prompt"),
		Model:            formData.Get("model"),
		Mode:             formData.Get("mode"),
		Image:            formData.Get("image"),
		Size:             formData.Get("size"),
		Ratio:            formData.Get("ratio"),
		AspectRatio:      formData.Get("aspect_ratio"),
		AspectRatioAlias: formData.Get("aspectRatio"),
		Resolution:       formData.Get("resolution"),
		Metadata:         make(map[string]interface{}),
	}

	if durationStr := formData.Get("seconds"); durationStr != "" {
		if duration, err := strconv.Atoi(durationStr); err == nil {
			req.Duration = duration
		}
	}

	if images := formData["images"]; len(images) > 0 {
		req.Images = images
	}

	for key, values := range formData {
		if len(values) > 0 && !isKnownTaskField(key) {
			if intVal, err := strconv.Atoi(values[0]); err == nil {
				req.Metadata[key] = intVal
			} else if floatVal, err := strconv.ParseFloat(values[0], 64); err == nil {
				req.Metadata[key] = floatVal
			} else {
				req.Metadata[key] = values[0]
			}
		}
	}
	return req, nil
}

func ValidateMultipartDirect(c *gin.Context, info *RelayInfo) *dto.TaskError {
	var prompt string
	var hasInputReference bool

	var req TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_json", http.StatusBadRequest, true)
	}

	prompt = req.Prompt
	if _, err := NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return createTaskError(err, "invalid_video_output", http.StatusBadRequest, true)
	}

	if strings.TrimSpace(req.Model) == "" {
		return createTaskError(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest, true)
	}

	if req.HasImage() {
		hasInputReference = true
	}

	if taskErr := validatePrompt(prompt); taskErr != nil {
		return taskErr
	}

	action := constant.TaskActionTextGenerate
	if hasInputReference {
		action = constant.TaskActionGenerate
	}

	storeTaskRequest(c, info, action, req)

	return nil
}

func isKnownTaskField(field string) bool {
	knownFields := map[string]bool{
		"prompt":          true,
		"model":           true,
		"mode":            true,
		"image":           true,
		"images":          true,
		"image_urls":      true,
		"video":           true,
		"videos":          true,
		"video_url":       true,
		"video_urls":      true,
		"audio":           true,
		"audios":          true,
		"audio_url":       true,
		"audio_urls":      true,
		"size":            true,
		"ratio":           true,
		"aspect_ratio":    true,
		"aspectRatio":     true,
		"resolution":      true,
		"duration":        true,
		"seconds":         true,
		"input_reference": true, // Sora 特有字段
	}
	return knownFields[field]
}

func ValidateBasicTaskRequest(c *gin.Context, info *RelayInfo, action string) *dto.TaskError {
	var err error
	contentType := c.GetHeader("Content-Type")
	var req TaskSubmitReq
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req, err = validateMultipartTaskRequest(c, info, action)
		if err != nil {
			return createTaskError(err, "invalid_multipart_form", http.StatusBadRequest, true)
		}
	}
	// 为了metadata字段的兼容性，统一UnmarshalBodyReusable
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_request", http.StatusBadRequest, true)
	}
	if _, err := NormalizeTaskSubmitVideoOutput(&req); err != nil {
		return createTaskError(err, "invalid_video_output", http.StatusBadRequest, true)
	}

	if taskErr := validatePrompt(req.Prompt); taskErr != nil {
		return taskErr
	}

	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		// 兼容单图上传
		req.Images = []string{req.Image}
	}

	storeTaskRequest(c, info, action, req)
	return nil
}

// ValidateTaskMultipartFiles prevents an adaptor from silently dropping a
// binary multipart part. URL and data-URI values are still decoded through
// TaskSubmitReq as usual. Every allowed field below is consumed by its
// adaptor; all other task channels fail before billing.
func ValidateTaskMultipartFiles(c *gin.Context, info *RelayInfo) error {
	if c == nil || !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return nil
	}

	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return err
	}
	defer form.RemoveAll()

	unsupported := make([]string, 0, len(form.File))
	for field, files := range form.File {
		if len(files) > 0 && !channelAllowsTaskMultipartFile(info, field) {
			unsupported = append(unsupported, field)
		}
	}
	if len(unsupported) == 0 {
		return nil
	}
	sort.Strings(unsupported)

	return fmt.Errorf(
		"binary multipart uploads for %s are not supported by this channel; use URL or data URI values, or a channel-specific file field instead",
		strings.Join(unsupported, ", "),
	)
}

func channelAllowsTaskMultipartFile(info *RelayInfo, field string) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	switch info.ChannelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora, constant.ChannelTypeShishi:
		return isOpenAICompatibleVideoFileField(field)
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi, constant.ChannelTypeJimeng:
		return field == "input_reference"
	case constant.ChannelTypeJimengDimensio:
		return isJimengDimensioFileField(field)
	default:
		return false
	}
}

// isOpenAICompatibleVideoFileField lists the binary fields that the
// Sora-compatible adaptors deliberately preserve. The aliases are retained
// for existing clients, but arbitrary multipart files must not bypass media
// validation merely because the provider body is rebuilt transparently.
func isOpenAICompatibleVideoFileField(field string) bool {
	switch field {
	case "image", "images", "image_urls", "input_reference",
		"video", "videos", "video_url", "video_urls",
		"audio", "audios", "audio_url", "audio_urls":
		return true
	default:
		return false
	}
}

func isJimengDimensioFileField(field string) bool {
	for prefix, max := range map[string]int{
		"image_file_": 9,
		"video_file_": 3,
		"audio_file_": 3,
	} {
		if !strings.HasPrefix(field, prefix) {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(field, prefix))
		return err == nil && index >= 1 && index <= max
	}
	return false
}
