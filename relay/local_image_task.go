package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
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

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/images/generations", bytes.NewReader(privateData.Request))
	c.Request.Header.Set("Content-Type", "application/json")
	setLocalImageContext(c, task, ch, key, proxy)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesGenerations,
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

func buildLocalImageRequestBody(c *gin.Context, adaptor channel.Adaptor, info *relaycommon.RelayInfo, imageReq dto.ImageRequest) (io.Reader, error) {
	convertedRequest, err := adaptor.ConvertImageRequest(c, info, imageReq)
	if err != nil {
		return nil, fmt.Errorf("convert local image request failed: %w", err)
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	if buffer, ok := convertedRequest.(*bytes.Buffer); ok {
		return buffer, nil
	}
	jsonData, err := common.Marshal(convertedRequest)
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
