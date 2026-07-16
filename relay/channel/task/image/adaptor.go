package image

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	aliapi "github.com/QuantumNous/new-api/relay/channel/ali"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

const imageTaskRequestKey = "image_task_request"

var localAsyncSubmitBody = []byte(`{"local_async":true}`)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	channelType int
	apiKey      string
	baseURL     string
	nativeAsync bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.channelType = info.ChannelType
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
	a.nativeAsync = false
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info.RelayMode != relayconstant.RelayModeImagesGenerations {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported async image relay mode: %d", info.RelayMode), "unsupported_relay_mode", http.StatusBadRequest)
	}

	imageReq, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	a.refreshNativeAsync(info, imageReq.Model)

	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.Action = constant.TaskActionImageGenerate
	info.Request = imageReq
	c.Set(imageTaskRequestKey, imageReq)
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	imageReq, err := getImageTaskRequest(c)
	if err != nil {
		return nil
	}

	ratios := map[string]float64{}
	imageN := lo.FromPtrOr(imageReq.N, uint(1))
	if imageN > 0 {
		ratios["n"] = float64(imageN)
	}

	if a.channelType == constant.ChannelTypeAli {
		aliReq, err := convertToAliImageRequest(info, *imageReq)
		if err != nil {
			return ratios
		}
		if aliReq.Parameters.PromptExtendValue() {
			ratios["prompt_extend"] = 2
		}
	}
	return ratios
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (a *TaskAdaptor) refreshNativeAsync(info *relaycommon.RelayInfo, requestModel string) {
	effectiveModel := firstNonEmpty(upstreamModelName(info), requestModel)
	a.nativeAsync = a.channelType == constant.ChannelTypeAli && !model_setting.IsSyncImageModel(effectiveModel)
}

func upstreamModelName(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return info.UpstreamModelName
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if !a.nativeAsync {
		return "", nil
	}
	return fmt.Sprintf("%s/api/v1/services/aigc/text2image/image-synthesis", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if a.nativeAsync {
		req.Header.Set("X-DashScope-Async", "enable")
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	imageReq, err := getImageTaskRequest(c)
	if err != nil {
		return nil, err
	}

	a.refreshNativeAsync(info, imageReq.Model)

	if !a.nativeAsync {
		bodyBytes, err := imageReq.MarshalJSONWithExtra()
		if err != nil {
			return nil, errors.Wrap(err, "marshal_local_image_request_failed")
		}
		return bytes.NewReader(bodyBytes), nil
	}

	aliReq, err := convertToAliImageRequest(info, *imageReq)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_image_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	if !a.nativeAsync {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(localAsyncSubmitBody)),
			Header:     make(http.Header),
		}, nil
	}
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	if !a.nativeAsync {
		c.JSON(http.StatusOK, gin.H{
			"id":      info.PublicTaskID,
			"object":  "image.task",
			"created": common.GetTimestamp(),
			"model":   info.OriginModelName,
			"status":  "queued",
			"task_id": info.PublicTaskID,
		})
		return info.PublicTaskID, localAsyncSubmitBody, nil
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var aliResp aliapi.AliResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if aliResp.Code != "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
	}
	if aliResp.Output.TaskId == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      info.PublicTaskID,
		"object":  "image.task",
		"created": common.GetTimestamp(),
		"model":   info.OriginModelName,
		"status":  mapAliImageStatus(aliResp.Output.TaskStatus),
		"task_id": info.PublicTaskID,
	})

	return aliResp.Output.TaskId, responseBody, nil
}

func (a *TaskAdaptor) BuildPrivateData(c *gin.Context, info *relaycommon.RelayInfo) (*model.TaskPrivateData, error) {
	if a.nativeAsync {
		return nil, nil
	}

	imageReq, err := getImageTaskRequest(c)
	if err != nil {
		return nil, err
	}
	request := *imageReq
	request.Stream = nil
	if mappedModel := upstreamModelName(info); mappedModel != "" {
		request.Model = mappedModel
	}
	requestBytes, err := request.MarshalJSONWithExtra()
	if err != nil {
		return nil, errors.Wrap(err, "marshal_local_image_task_request_failed")
	}

	return &model.TaskPrivateData{
		Key: info.ApiKey,
		LocalImageTask: &model.LocalImageTaskPrivateData{
			Request:     requestBytes,
			ChannelType: info.ChannelType,
			APIType:     info.ApiType,
			BaseURL:     info.ChannelBaseUrl,
			APIVersion:  info.ApiVersion,
		},
	}, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return nil
}

func (a *TaskAdaptor) GetChannelName() string {
	return "Async Image"
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID), nil)
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

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	if taskResult, ok := parseLocalImageTaskResult(respBody); ok {
		return taskResult, nil
	}

	var aliResp aliapi.AliResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal async image task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: 0}
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = string(model.TaskStatusQueued)
	case "RUNNING":
		taskResult.Status = string(model.TaskStatusInProgress)
	case "SUCCEEDED":
		taskResult.Status = string(model.TaskStatusSuccess)
		taskResult.Url = firstImageResultURL(aliResp.Output.Results)
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = string(model.TaskStatusFailure)
		taskResult.Reason = firstNonEmpty(aliResp.Output.Message, aliResp.Message, "task failed")
	default:
		return nil, fmt.Errorf("unknown Ali image task status %q", aliResp.Output.TaskStatus)
	}
	return &taskResult, nil
}

func parseLocalImageTaskResult(respBody []byte) (*relaycommon.TaskInfo, bool) {
	var localResp struct {
		Status     string `json:"status"`
		ResultURL  string `json:"result_url"`
		FailReason string `json:"fail_reason"`
	}
	if err := common.Unmarshal(respBody, &localResp); err != nil || strings.TrimSpace(localResp.Status) == "" {
		return nil, false
	}
	return &relaycommon.TaskInfo{
		Code:   0,
		Status: localResp.Status,
		Url:    localResp.ResultURL,
		Reason: localResp.FailReason,
	}, true
}

func getImageTaskRequest(c *gin.Context) (*dto.ImageRequest, error) {
	value, ok := c.Get(imageTaskRequestKey)
	if !ok {
		return nil, fmt.Errorf("image task request not found")
	}
	imageReq, ok := value.(*dto.ImageRequest)
	if !ok || imageReq == nil {
		return nil, fmt.Errorf("invalid image task request")
	}
	return imageReq, nil
}

func convertToAliImageRequest(info *relaycommon.RelayInfo, request dto.ImageRequest) (*aliapi.AliImageRequest, error) {
	aliReq := &aliapi.AliImageRequest{
		Model:          firstNonEmpty(info.UpstreamModelName, request.Model),
		ResponseFormat: request.ResponseFormat,
	}

	if request.Extra != nil {
		if val, ok := request.Extra["parameters"]; ok {
			if err := common.Unmarshal(val, &aliReq.Parameters); err != nil {
				return nil, fmt.Errorf("invalid parameters field: %w", err)
			}
		} else {
			aliReq.Parameters = aliapi.AliImageParameters{
				Size:      strings.ReplaceAll(request.Size, "x", "*"),
				N:         int(lo.FromPtrOr(request.N, uint(1))),
				Watermark: request.Watermark,
			}
		}
		if val, ok := request.Extra["input"]; ok {
			var input aliapi.AliImageInput
			if err := common.Unmarshal(val, &input); err != nil {
				return nil, fmt.Errorf("invalid input field: %w", err)
			}
			aliReq.Input = input
		}
	} else {
		aliReq.Parameters = aliapi.AliImageParameters{
			Size:      strings.ReplaceAll(request.Size, "x", "*"),
			N:         int(lo.FromPtrOr(request.N, uint(1))),
			Watermark: request.Watermark,
		}
	}

	if aliReq.Input == nil {
		aliReq.Input = aliapi.AliImageInput{Prompt: request.Prompt}
	}
	return aliReq, nil
}

func firstImageResultURL(results []aliapi.TaskResult) string {
	for _, result := range results {
		if strings.TrimSpace(result.Url) != "" {
			return result.Url
		}
	}
	for _, result := range results {
		image := strings.TrimSpace(result.B64Image)
		if image == "" {
			continue
		}
		if strings.HasPrefix(image, "data:") {
			return image
		}
		return "data:image/png;base64," + image
	}
	return ""
}

func mapAliImageStatus(status string) string {
	switch status {
	case "PENDING":
		return "queued"
	case "RUNNING":
		return "in_progress"
	case "SUCCEEDED":
		return "completed"
	case "FAILED", "CANCELED", "UNKNOWN":
		return "failed"
	default:
		return "queued"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
