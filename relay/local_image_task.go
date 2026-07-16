package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	localImageGenerationPath = "/v1/images/generations"
	localImageEditPath       = "/v1/images/edits"
	localImageMaxReferences  = 16
)

type localImagePollResponse struct {
	Status     string            `json:"status"`
	ResultURL  string            `json:"result_url,omitempty"`
	FailReason string            `json:"fail_reason,omitempty"`
	Data       dto.ImageResponse `json:"data,omitempty"`
}

type localImageTransientError struct {
	err error
}

func (e localImageTransientError) Error() string {
	return e.err.Error()
}

func (e localImageTransientError) Unwrap() error {
	return e.err
}

func ExecuteLocalImageTask(ctx context.Context, task *model.Task, ch *model.Channel, key string, proxy string) (*http.Response, error) {
	result, err := executeLocalImageTask(ctx, task, ch, key, proxy)
	if err != nil {
		if isLocalImageTransientError(err) {
			return nil, err
		}
		result = localImagePollResponse{
			Status:     string(model.TaskStatusFailure),
			FailReason: err.Error(),
		}
	}
	body, err := common.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func newLocalImageTransientError(message string, err error) error {
	if err == nil {
		err = errors.New(message)
	} else {
		err = fmt.Errorf("%s: %w", message, err)
	}
	return localImageTransientError{err: err}
}

func isLocalImageTransientError(err error) bool {
	var transient localImageTransientError
	return errors.As(err, &transient)
}

func isLocalImageSuccessStatus(statusCode int, apiType int) bool {
	if statusCode == http.StatusOK {
		return true
	}
	return statusCode == http.StatusCreated && apiType == constant.APITypeReplicate
}

func isLocalImageTransientStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func executeLocalImageTask(ctx context.Context, task *model.Task, ch *model.Channel, key string, proxy string) (localImagePollResponse, error) {
	if task == nil || task.PrivateData.LocalImageTask == nil {
		return localImagePollResponse{}, fmt.Errorf("local image task private data is empty")
	}
	privateData := task.PrivateData.LocalImageTask
	apiType := privateData.APIType
	if apiType == 0 {
		resolvedAPIType, ok := common.ChannelType2APIType(privateData.ChannelType)
		if !ok {
			return localImagePollResponse{}, fmt.Errorf("invalid image channel type: %d", privateData.ChannelType)
		}
		apiType = resolvedAPIType
	}
	adaptor := GetAdaptor(apiType)
	if adaptor == nil {
		return localImagePollResponse{}, fmt.Errorf("invalid image api type: %d", apiType)
	}

	var imageReq dto.ImageRequest
	if err := common.Unmarshal(privateData.Request, &imageReq); err != nil {
		return localImagePollResponse{}, fmt.Errorf("unmarshal local image request failed: %w", err)
	}

	relayMode, requestPath := localImageRequestMode(apiType, imageReq)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, requestPath, bytes.NewReader(privateData.Request))
	c.Request.Header.Set("Content-Type", "application/json")
	setLocalImageContext(c, task, ch, key, proxy)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayMode,
		RelayFormat:     types.RelayFormatOpenAIImage,
		OriginModelName: task.Properties.OriginModelName,
		RequestURLPath:  c.Request.URL.Path,
		StartTime:       time.Now(),
	}
	if info.OriginModelName == "" {
		info.OriginModelName = imageReq.Model
	}
	info.InitChannelMeta(c)
	info.Request = &imageReq
	info.UpstreamModelName = imageReq.Model
	adaptor.Init(info)

	requestBody, err := buildLocalImageRequestBody(c, adaptor, info, imageReq)
	if err != nil {
		return localImagePollResponse{}, err
	}
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return localImagePollResponse{}, newLocalImageTransientError("do local image request failed", err)
	}
	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		return localImagePollResponse{}, fmt.Errorf("local image upstream returned invalid response")
	}
	if !isLocalImageSuccessStatus(httpResp.StatusCode, apiType) {
		message := readLocalImageError(httpResp, info)
		err := fmt.Errorf("local image upstream error: %s", message)
		if isLocalImageTransientStatus(httpResp.StatusCode) {
			return localImagePollResponse{}, localImageTransientError{err: err}
		}
		return localImagePollResponse{}, err
	}
	if httpResp.StatusCode == http.StatusCreated && apiType == constant.APITypeReplicate {
		httpResp.StatusCode = http.StatusOK
	}
	if _, newAPIError := adaptor.DoResponse(c, httpResp, info); newAPIError != nil {
		err := newAPIError.Err
		if err == nil {
			err = fmt.Errorf("local image upstream response error: %s", newAPIError.Error())
		}
		if isLocalImageTransientStatus(newAPIError.StatusCode) {
			return localImagePollResponse{}, localImageTransientError{err: err}
		}
		return localImagePollResponse{}, err
	}

	var imageResp dto.ImageResponse
	responseBody := recorder.Body.Bytes()
	if err := common.Unmarshal(responseBody, &imageResp); err != nil {
		return localImagePollResponse{}, fmt.Errorf("unmarshal local image response failed: %w", err)
	}
	resultURL := firstLocalImageResultURL(imageResp.Data)
	if resultURL == "" {
		return localImagePollResponse{}, fmt.Errorf("local image response contains no image result")
	}
	return localImagePollResponse{
		Status:    string(model.TaskStatusSuccess),
		ResultURL: resultURL,
		Data:      imageResp,
	}, nil
}

func localImageRequestMode(apiType int, imageReq dto.ImageRequest) (int, string) {
	if apiType == constant.APITypeOpenAI && hasLocalImageReference(imageReq) {
		return relayconstant.RelayModeImagesEdits, localImageEditPath
	}
	return relayconstant.RelayModeImagesGenerations, localImageGenerationPath
}

func hasLocalImageReference(imageReq dto.ImageRequest) bool {
	return rawJSONHasValue(imageReq.Images) || rawJSONHasValue(imageReq.Image)
}

func rawJSONHasValue(raw []byte) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != `""` && value != "[]" && value != "{}"
}

func buildLocalImageRequestBody(c *gin.Context, adaptor channel.Adaptor, info *relaycommon.RelayInfo, imageReq dto.ImageRequest) (io.Reader, error) {
	if err := normalizeLocalImageRequest(info.ApiType, &imageReq); err != nil {
		return nil, err
	}
	if info.ApiType == constant.APITypeOpenAI && info.RelayMode == relayconstant.RelayModeImagesEdits && isLocalImageJSONRequest(c) {
		return buildLocalOpenAIImageEditMultipart(c, imageReq)
	}
	convertedRequest, err := adaptor.ConvertImageRequest(c, info, imageReq)
	if err != nil {
		return nil, fmt.Errorf("convert local image request failed: %w", err)
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	if buffer, ok := convertedRequest.(*bytes.Buffer); ok {
		return buffer, nil
	}
	jsonData, err := marshalLocalImageConvertedRequest(convertedRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal local image request failed: %w", err)
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return bytes.NewReader(jsonData), nil
}

func isLocalImageJSONRequest(c *gin.Context) bool {
	return c != nil && c.Request != nil && strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json")
}

func buildLocalOpenAIImageEditMultipart(c *gin.Context, request dto.ImageRequest) (io.Reader, error) {
	imageURLs, err := localImageReferenceURLs(request)
	if err != nil {
		return nil, err
	}
	if len(imageURLs) == 0 {
		return nil, fmt.Errorf("OpenAI image edit requires at least one reference image")
	}
	if len(imageURLs) > localImageMaxReferences {
		return nil, fmt.Errorf("OpenAI image edit supports at most %d reference images", localImageMaxReferences)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeField := func(name string, value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return writer.WriteField(name, value)
	}
	if err := writeField("model", request.Model); err != nil {
		return nil, err
	}
	if err := writeField("prompt", request.Prompt); err != nil {
		return nil, err
	}
	if request.N != nil {
		if err := writeField("n", strconv.FormatUint(uint64(*request.N), 10)); err != nil {
			return nil, err
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "size", value: request.Size},
		{name: "quality", value: request.Quality},
		{name: "response_format", value: request.ResponseFormat},
	} {
		if err := writeField(field.name, field.value); err != nil {
			return nil, err
		}
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "background", raw: request.Background},
		{name: "moderation", raw: request.Moderation},
		{name: "output_format", raw: request.OutputFormat},
		{name: "output_compression", raw: request.OutputCompression},
		{name: "input_fidelity", raw: request.InputFidelity},
		{name: "partial_images", raw: request.PartialImages},
		{name: "user", raw: request.User},
	} {
		value, err := localImageRawFieldValue(field.raw)
		if err != nil {
			return nil, fmt.Errorf("invalid OpenAI image field %s: %w", field.name, err)
		}
		if err := writeField(field.name, value); err != nil {
			return nil, err
		}
	}
	if request.Stream != nil {
		if err := writeField("stream", strconv.FormatBool(*request.Stream)); err != nil {
			return nil, err
		}
	}

	fieldName := "image"
	if len(imageURLs) > 1 {
		fieldName = "image[]"
	}
	for index, imageURL := range imageURLs {
		mimeType, encoded, err := service.GetImageFromUrlWithContext(c.Request.Context(), imageURL)
		if err != nil {
			return nil, fmt.Errorf("download OpenAI reference image %d failed: %w", index+1, err)
		}
		imageBytes, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode OpenAI reference image %d failed: %w", index+1, err)
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="reference-%d.%s"`, fieldName, index+1, localImageExtension(mimeType)))
		header.Set("Content-Type", strings.TrimSpace(strings.Split(mimeType, ";")[0]))
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, fmt.Errorf("create OpenAI reference image %d part failed: %w", index+1, err)
		}
		if _, err := part.Write(imageBytes); err != nil {
			return nil, fmt.Errorf("write OpenAI reference image %d failed: %w", index+1, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &body, nil
}

func localImageReferenceURLs(request dto.ImageRequest) ([]string, error) {
	values := make([]string, 0)
	for _, raw := range []json.RawMessage{request.Images, request.Image} {
		if !rawJSONHasValue(raw) {
			continue
		}
		var value any
		if err := common.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("invalid local image reference: %w", err)
		}
		appendLocalImageReferenceURLs(&values, value)
	}
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique, nil
}

func appendLocalImageReferenceURLs(values *[]string, value any) {
	switch typed := value.(type) {
	case string:
		*values = append(*values, typed)
	case []any:
		for _, item := range typed {
			appendLocalImageReferenceURLs(values, item)
		}
	case map[string]any:
		for _, key := range []string{"image_url", "url"} {
			if item, ok := typed[key]; ok {
				appendLocalImageReferenceURLs(values, item)
				return
			}
		}
	}
}

func localImageRawFieldValue(raw json.RawMessage) (string, error) {
	if !rawJSONHasValue(raw) {
		return "", nil
	}
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	encoded, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func localImageExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "png"
	}
}

func normalizeLocalImageRequest(apiType int, request *dto.ImageRequest) error {
	if apiType != constant.APITypeOpenAI || request == nil || request.Extra == nil {
		return nil
	}
	parametersJSON, ok := request.Extra["parameters"]
	if !ok {
		return nil
	}

	var parameters map[string]json.RawMessage
	if err := common.Unmarshal(parametersJSON, &parameters); err != nil {
		return fmt.Errorf("invalid local image parameters: %w", err)
	}
	payloadJSON, err := request.MarshalJSONWithExtra()
	if err != nil {
		return fmt.Errorf("marshal local image request for normalization failed: %w", err)
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(payloadJSON, &payload); err != nil {
		return fmt.Errorf("unmarshal local image request for normalization failed: %w", err)
	}
	delete(payload, "parameters")
	for key, value := range parameters {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}

	normalizedJSON, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal normalized local image request failed: %w", err)
	}
	if err := common.Unmarshal(normalizedJSON, request); err != nil {
		return fmt.Errorf("unmarshal normalized local image request failed: %w", err)
	}
	return nil
}

func marshalLocalImageConvertedRequest(convertedRequest any) ([]byte, error) {
	switch request := convertedRequest.(type) {
	case dto.ImageRequest:
		return request.MarshalJSONWithExtra()
	case *dto.ImageRequest:
		if request == nil {
			return nil, fmt.Errorf("converted local image request is nil")
		}
		return request.MarshalJSONWithExtra()
	default:
		return common.Marshal(convertedRequest)
	}
}

func setLocalImageContext(c *gin.Context, task *model.Task, ch *model.Channel, key string, proxy string) {
	privateData := task.PrivateData.LocalImageTask
	baseURL := privateData.BaseURL
	if baseURL == "" {
		baseURL = ch.GetBaseURL()
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
	common.SetContextKey(c, constant.ContextKeyChannelType, privateData.ChannelType)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, ch.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, ch.GetOtherSettings())
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, ch.GetParamOverride())
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, ch.GetHeaderOverride())
	common.SetContextKey(c, constant.ContextKeyOriginalModel, task.Properties.UpstreamModelName)
	if proxy != "" {
		setting := ch.GetSetting()
		setting.Proxy = proxy
		common.SetContextKey(c, constant.ContextKeyChannelSetting, setting)
	}
	if ch.OpenAIOrganization != nil {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *ch.OpenAIOrganization)
	}
	if privateData.APIVersion != "" {
		c.Set("api_version", privateData.APIVersion)
	}
}

func readLocalImageError(resp *http.Response, info *relaycommon.RelayInfo) string {
	if resp.Body == nil {
		return resp.Status
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return sanitizeTaskUpstreamError([]byte(message), info)
}

func firstLocalImageResultURL(data []dto.ImageData) string {
	for _, image := range data {
		if strings.TrimSpace(image.Url) != "" {
			return image.Url
		}
	}
	for _, image := range data {
		b64 := strings.TrimSpace(image.B64Json)
		if b64 == "" {
			continue
		}
		if strings.HasPrefix(b64, "data:") {
			return b64
		}
		return "data:image/png;base64," + b64
	}
	return ""
}
