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

type TaskAdaptor struct {
	taskcommon.BaseBilling
	channelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.channelType = info.ChannelType
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if a.channelType != constant.ChannelTypeAli {
		return service.TaskErrorWrapperLocal(fmt.Errorf("async image task only supports Ali channel currently"), "unsupported_channel", http.StatusBadRequest)
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported async image relay mode: %d", info.RelayMode), "unsupported_relay_mode", http.StatusBadRequest)
	}

	imageReq, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if model_setting.IsSyncImageModel(imageReq.Model) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model %s does not support native async image task", imageReq.Model), "unsupported_model", http.StatusBadRequest)
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

	aliReq, err := convertToAliImageRequest(info, *imageReq)
	if err != nil {
		return ratios
	}
	if aliReq.Parameters.PromptExtendValue() {
		ratios["prompt_extend"] = 2
	}
	return ratios
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.channelType != constant.ChannelTypeAli {
		return "", fmt.Errorf("unsupported async image channel type: %d", a.channelType)
	}
	return fmt.Sprintf("%s/api/v1/services/aigc/text2image/image-synthesis", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	imageReq, err := getImageTaskRequest(c)
	if err != nil {
		return nil, err
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
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
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
		taskResult.Status = string(model.TaskStatusQueued)
	}
	return &taskResult, nil
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
