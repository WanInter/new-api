package tencentvod

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	ChannelName         = "tencent-vod"
	DefaultBaseURL      = "https://vod.tencentcloudapi.com"
	tencentCloudService = "vod"
	tencentCloudVersion = "2018-07-17"
	createTaskAction    = "CreateAigcVideoTask"
	describeTaskAction  = "DescribeTaskDetail"
	payloadContextKey   = "tencent_vod_request_payload"
	defaultDuration     = 5
	defaultResolution   = "720P"
	defaultAspectRatio  = "16:9"
	defaultStorageMode  = "Temporary"
	maxPromptLength     = 1000
)

var ModelList = []string{
	"kling-vod-1.6",
	"kling-vod-2.0",
	"kling-vod-2.1",
	"kling-vod-2.5",
	"kling-vod-2.6",
	"kling-vod-o1",
	"kling-vod-3.0",
	"kling-vod-3.0-omni",
}

var modelVersionAliases = map[string]string{
	"1.6":                   "1.6",
	"2.0":                   "2.0",
	"2.1":                   "2.1",
	"2.5":                   "2.5",
	"2.6":                   "2.6",
	"o1":                    "O1",
	"3.0":                   "3.0",
	"3.0-omni":              "3.0-Omni",
	"kling-vod-1.6":         "1.6",
	"kling-vod-2.0":         "2.0",
	"kling-vod-2.1":         "2.1",
	"kling-vod-2.5":         "2.5",
	"kling-vod-2.6":         "2.6",
	"kling-vod-o1":          "O1",
	"kling-vod-3.0":         "3.0",
	"kling-vod-3.0-omni":    "3.0-Omni",
	"tencent-vod-kling-1.6": "1.6",
	"kling-v1-6":            "1.6",
}

type credentials struct {
	SecretID  string
	SecretKey string
	SubAppID  uint64
}

type requestPayload struct {
	SubAppID        uint64            `json:"SubAppId"`
	ModelName       string            `json:"ModelName"`
	ModelVersion    string            `json:"ModelVersion"`
	FileInfos       []inputFileInfo   `json:"FileInfos,omitempty"`
	LastFrameFileID string            `json:"LastFrameFileId,omitempty"`
	LastFrameURL    string            `json:"LastFrameUrl,omitempty"`
	Prompt          string            `json:"Prompt,omitempty"`
	NegativePrompt  string            `json:"NegativePrompt,omitempty"`
	EnhancePrompt   string            `json:"EnhancePrompt,omitempty"`
	OutputConfig    videoOutputConfig `json:"OutputConfig"`
	InputRegion     string            `json:"InputRegion,omitempty"`
	SceneType       string            `json:"SceneType,omitempty"`
	Seed            *int64            `json:"Seed,omitempty"`
	SessionID       string            `json:"SessionId,omitempty"`
	SessionContext  string            `json:"SessionContext,omitempty"`
	TasksPriority   *int64            `json:"TasksPriority,omitempty"`
	ExtInfo         string            `json:"ExtInfo,omitempty"`
}

type inputFileInfo struct {
	Type              string `json:"Type"`
	Category          string `json:"Category"`
	FileID            string `json:"FileId,omitempty"`
	URL               string `json:"Url,omitempty"`
	Base64            string `json:"Base64,omitempty"`
	ReferenceType     string `json:"ReferenceType,omitempty"`
	KeepOriginalSound string `json:"KeepOriginalSound,omitempty"`
	Usage             string `json:"Usage,omitempty"`
}

type videoOutputConfig struct {
	StorageMode           string   `json:"StorageMode,omitempty"`
	MediaName             string   `json:"MediaName,omitempty"`
	ClassID               *int64   `json:"ClassId,omitempty"`
	ExpireTime            string   `json:"ExpireTime,omitempty"`
	Duration              *float64 `json:"Duration,omitempty"`
	Resolution            string   `json:"Resolution,omitempty"`
	AspectRatio           string   `json:"AspectRatio,omitempty"`
	AudioGeneration       string   `json:"AudioGeneration,omitempty"`
	PersonGeneration      string   `json:"PersonGeneration,omitempty"`
	InputComplianceCheck  string   `json:"InputComplianceCheck,omitempty"`
	OutputComplianceCheck string   `json:"OutputComplianceCheck,omitempty"`
	EnhanceSwitch         string   `json:"EnhanceSwitch,omitempty"`
	OffPeak               string   `json:"OffPeak,omitempty"`
}

type requestMetadata struct {
	NegativePrompt        string          `json:"negative_prompt"`
	EnhancePrompt         any             `json:"enhance_prompt"`
	Resolution            string          `json:"resolution"`
	AspectRatio           string          `json:"aspect_ratio"`
	StorageMode           string          `json:"storage_mode"`
	MediaName             string          `json:"media_name"`
	ClassID               *int64          `json:"class_id"`
	ExpireTime            string          `json:"expire_time"`
	AudioGeneration       any             `json:"audio_generation"`
	PersonGeneration      string          `json:"person_generation"`
	InputComplianceCheck  string          `json:"input_compliance_check"`
	OutputComplianceCheck string          `json:"output_compliance_check"`
	EnhanceSwitch         any             `json:"enhance_switch"`
	OffPeak               any             `json:"off_peak"`
	InputRegion           string          `json:"input_region"`
	SceneType             string          `json:"scene_type"`
	Seed                  *int64          `json:"seed"`
	SessionContext        string          `json:"session_context"`
	TasksPriority         *int64          `json:"tasks_priority"`
	ExtInfo               string          `json:"ext_info"`
	ImageUsage            string          `json:"image_usage"`
	LastFrameURL          string          `json:"last_frame_url"`
	ImageTail             string          `json:"image_tail"`
	LastFrameFileID       string          `json:"last_frame_file_id"`
	FileInfos             []inputFileInfo `json:"file_infos"`
}

type cloudError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type submitResponse struct {
	Response struct {
		Error     *cloudError `json:"Error,omitempty"`
		TaskID    string      `json:"TaskId"`
		RequestID string      `json:"RequestId"`
	} `json:"Response"`
}

type describeResponse struct {
	Response struct {
		Error         *cloudError    `json:"Error,omitempty"`
		RequestID     string         `json:"RequestId"`
		TaskType      string         `json:"TaskType"`
		Status        string         `json:"Status"`
		AigcVideoTask *aigcVideoTask `json:"AigcVideoTask,omitempty"`
	} `json:"Response"`
}

type aigcVideoTask struct {
	TaskID     string          `json:"TaskId"`
	Status     string          `json:"Status"`
	ErrCode    int64           `json:"ErrCode"`
	ErrCodeExt string          `json:"ErrCodeExt"`
	Message    string          `json:"Message"`
	Progress   int             `json:"Progress"`
	Output     videoTaskOutput `json:"Output"`
}

type videoTaskOutput struct {
	FileInfos []outputFileInfo `json:"FileInfos"`
}

type outputFileInfo struct {
	StorageMode string `json:"StorageMode"`
	FileType    string `json:"FileType"`
	FileURL     string `json:"FileUrl"`
	FileID      string `json:"FileId"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		a.apiKey = ""
		a.baseURL = DefaultBaseURL
		return
	}
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil || info.ChannelMeta == nil {
		return service.TaskErrorWrapperLocal(errors.New("relay info is required"), "invalid_request", http.StatusBadRequest)
	}
	creds, err := parseCredentials(info.ApiKey)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_channel_key", http.StatusBadRequest)
	}
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if utf8.RuneCountInString(req.Prompt) > maxPromptLength {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt must not exceed %d characters", maxPromptLength), "invalid_request", http.StatusBadRequest)
	}
	if _, err := convertToRequestPayload(&req, info, creds.SubAppID); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	info.Action = constant.TaskActionTextGenerate
	if len(req.Images) > 0 || strings.TrimSpace(req.InputReference) != "" {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	duration, err := normalizeDuration(req.Duration, req.Seconds)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(duration)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	creds, err := parseCredentials(a.apiKey)
	if err != nil {
		return err
	}
	payload, ok := c.Get(payloadContextKey)
	if !ok {
		return errors.New("tencent vod request payload missing from context")
	}
	body, ok := payload.([]byte)
	if !ok {
		return errors.New("invalid tencent vod request payload")
	}
	return signRequest(req, body, creds, createTaskAction, time.Now())
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	creds, err := parseCredentials(a.apiKey)
	if err != nil {
		return nil, err
	}
	payload, err := convertToRequestPayload(&req, info, creds.SubAppID)
	if err != nil {
		return nil, err
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c.Set(payloadContextKey, body)
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	if resp == nil || resp.Body == nil {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("tencent vod returned an empty response"), "empty_response", http.StatusBadGateway)
	}
	if info == nil || info.TaskRelayInfo == nil {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("relay info is required"), "invalid_request", http.StatusInternalServerError)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()
	var parsed submitResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if parsed.Response.Error != nil {
		return "", nil, service.TaskErrorWrapperLocal(
			fmt.Errorf("tencent vod submit failed: %s: %s", parsed.Response.Error.Code, parsed.Response.Error.Message),
			parsed.Response.Error.Code,
			http.StatusBadRequest,
		)
	}
	if strings.TrimSpace(parsed.Response.TaskID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("tencent vod returned an empty task ID"), "submit_failed", http.StatusBadGateway)
	}
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	ov.Status = dto.VideoStatusQueued
	c.JSON(http.StatusOK, ov)
	return parsed.Response.TaskID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task_id")
	}
	creds, err := parseCredentials(key)
	if err != nil {
		return nil, err
	}
	payload, err := common.Marshal(map[string]any{
		"SubAppId": creds.SubAppID,
		"TaskId":   taskID,
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(baseURL, "/")
	if endpoint == "" {
		endpoint = DefaultBaseURL
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if err := signRequest(req, payload, creds, describeTaskAction, time.Now()); err != nil {
		return nil, err
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var parsed describeResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal tencent vod task result failed")
	}
	if parsed.Response.Error != nil {
		return nil, fmt.Errorf("tencent vod task query failed: %s: %s", parsed.Response.Error.Code, parsed.Response.Error.Message)
	}
	task := parsed.Response.AigcVideoTask
	if task == nil {
		return nil, errors.New("tencent vod response did not contain AigcVideoTask")
	}
	info := &relaycommon.TaskInfo{
		Code:     int(task.ErrCode),
		TaskID:   task.TaskID,
		Progress: fmt.Sprintf("%d%%", normalizedProgress(task.Progress)),
	}
	if taskFailed(task, parsed.Response.Status) {
		info.Status = model.TaskStatusFailure
		info.Progress = taskcommon.ProgressComplete
		info.Reason = firstNonEmpty(task.Message, task.ErrCodeExt, "Tencent VOD AIGC task failed")
		return info, nil
	}
	status := firstNonEmpty(task.Status, parsed.Response.Status)
	switch strings.ToUpper(status) {
	case "WAITING":
		info.Status = model.TaskStatusQueued
		if task.Progress == 0 {
			info.Progress = taskcommon.ProgressQueued
		}
	case "PROCESSING":
		info.Status = model.TaskStatusInProgress
		if task.Progress == 0 {
			info.Progress = taskcommon.ProgressInProgress
		}
	case "FINISH":
		info.Url = firstOutputURL(task.Output.FileInfos)
		if info.Url == "" {
			info.Status = model.TaskStatusFailure
			info.Reason = firstNonEmpty(task.Message, "Tencent VOD AIGC task completed without a video URL")
		} else {
			info.Status = model.TaskStatusSuccess
		}
		info.Progress = taskcommon.ProgressComplete
	case "ABORTED":
		info.Status = model.TaskStatusFailure
		info.Progress = taskcommon.ProgressComplete
		info.Reason = firstNonEmpty(task.Message, "Tencent VOD AIGC task was aborted")
	default:
		return nil, fmt.Errorf("unknown Tencent VOD task status %q", status)
	}
	return info, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	if originTask == nil {
		return nil, errors.New("task is required")
	}
	ov := originTask.ToOpenAIVideo()
	var parsed describeResponse
	if len(originTask.Data) > 0 {
		_ = common.Unmarshal(originTask.Data, &parsed)
	}
	if parsed.Response.AigcVideoTask != nil {
		if outputURL := firstOutputURL(parsed.Response.AigcVideoTask.Output.FileInfos); outputURL != "" {
			ov.SetMetadata("url", outputURL)
			ov.SetMetadata("video_url", outputURL)
			ov.SetMetadata("result_url", outputURL)
		}
	}
	if originTask.Status == model.TaskStatusFailure {
		ov.Error = &dto.OpenAIVideoError{Message: originTask.FailReason, Code: "tencent_vod_task_failed"}
	}
	return common.Marshal(ov)
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo, subAppID uint64) (*requestPayload, error) {
	modelVersion, err := resolveModelVersion(req.Model, info)
	if err != nil {
		return nil, err
	}
	duration, err := normalizeDuration(req.Duration, req.Seconds)
	if err != nil {
		return nil, err
	}
	metadata := requestMetadata{}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &metadata); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(req.Prompt) > maxPromptLength {
		return nil, fmt.Errorf("prompt must not exceed %d characters", maxPromptLength)
	}
	if utf8.RuneCountInString(metadata.NegativePrompt) > maxPromptLength {
		return nil, fmt.Errorf("negative_prompt must not exceed %d characters", maxPromptLength)
	}
	resolution, err := normalizeResolution(metadata.Resolution)
	if err != nil {
		return nil, err
	}
	fileInfos, lastFrameURL, err := buildFileInputs(req, metadata)
	if err != nil {
		return nil, err
	}
	hasLastFrame := lastFrameURL != "" || strings.TrimSpace(metadata.LastFrameFileID) != ""
	if hasLastFrame && len(fileInfos) == 0 {
		return nil, errors.New("Tencent VOD Kling tail-frame generation requires a first-frame image")
	}
	if lastFrameURL != "" && !isHTTPURL(lastFrameURL) {
		return nil, errors.New("Tencent VOD Kling last_frame_url must be an HTTP(S) URL")
	}
	if hasLastFrame && (modelVersion != "2.1" || resolution != "1080P") {
		return nil, errors.New("Kling tail-frame generation on Tencent VOD requires model version 2.1 and resolution 1080P")
	}
	durationFloat := float64(duration)
	aspectRatio, err := normalizeAspectRatio(
		firstNonEmpty(metadata.AspectRatio, aspectRatioFromSize(req.Size)),
		len(fileInfos) == 0,
	)
	if err != nil {
		return nil, err
	}
	payload := &requestPayload{
		SubAppID:        subAppID,
		ModelName:       "Kling",
		ModelVersion:    modelVersion,
		FileInfos:       fileInfos,
		LastFrameFileID: strings.TrimSpace(metadata.LastFrameFileID),
		LastFrameURL:    lastFrameURL,
		Prompt:          req.Prompt,
		NegativePrompt:  metadata.NegativePrompt,
		EnhancePrompt:   normalizeEnabled(metadata.EnhancePrompt),
		OutputConfig: videoOutputConfig{
			StorageMode:           normalizeStorageMode(metadata.StorageMode),
			MediaName:             metadata.MediaName,
			ClassID:               metadata.ClassID,
			ExpireTime:            metadata.ExpireTime,
			Duration:              &durationFloat,
			Resolution:            resolution,
			AspectRatio:           aspectRatio,
			AudioGeneration:       normalizeEnabled(metadata.AudioGeneration),
			PersonGeneration:      metadata.PersonGeneration,
			InputComplianceCheck:  metadata.InputComplianceCheck,
			OutputComplianceCheck: metadata.OutputComplianceCheck,
			EnhanceSwitch:         normalizeEnabled(metadata.EnhanceSwitch),
			OffPeak:               normalizeEnabled(metadata.OffPeak),
		},
		InputRegion:    metadata.InputRegion,
		SceneType:      metadata.SceneType,
		Seed:           metadata.Seed,
		SessionContext: metadata.SessionContext,
		TasksPriority:  metadata.TasksPriority,
		ExtInfo:        metadata.ExtInfo,
	}
	if info != nil && info.TaskRelayInfo != nil {
		payload.SessionID = info.PublicTaskID
	}
	return payload, nil
}

func parseCredentials(key string) (credentials, error) {
	parts := strings.Split(key, "|")
	if len(parts) != 3 {
		return credentials{}, errors.New("invalid Tencent VOD key: expected SecretId|SecretKey|SubAppId")
	}
	creds := credentials{
		SecretID:  strings.TrimSpace(parts[0]),
		SecretKey: strings.TrimSpace(parts[1]),
	}
	if creds.SecretID == "" || creds.SecretKey == "" {
		return credentials{}, errors.New("invalid Tencent VOD key: SecretId and SecretKey are required")
	}
	subAppID, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
	if err != nil || subAppID == 0 {
		return credentials{}, errors.New("invalid Tencent VOD key: SubAppId must be a positive integer")
	}
	creds.SubAppID = subAppID
	return creds, nil
}

func signRequest(req *http.Request, body []byte, creds credentials, action string, now time.Time) error {
	if req == nil || req.URL == nil {
		return errors.New("invalid Tencent Cloud request")
	}
	host := req.URL.Host
	if host == "" {
		return errors.New("Tencent Cloud request host is empty")
	}
	contentType := "application/json; charset=utf-8"
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalHeaders := fmt.Sprintf(
		"content-type:%s\nhost:%s\nx-tc-action:%s\n",
		contentType,
		host,
		strings.ToLower(action),
	)
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery(req.URL),
		canonicalHeaders,
		signedHeaders,
		sha256Hex(body),
	}, "\n")
	timestamp := now.Unix()
	date := now.UTC().Format("2006-01-02")
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, tencentCloudService)
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(timestamp, 10),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	secretDate := hmacSHA256([]byte("TC3"+creds.SecretKey), date)
	secretService := hmacSHA256(secretDate, tencentCloudService)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	authorization := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.SecretID,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Host = host
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-TC-Version", tencentCloudVersion)
	return nil
}

func canonicalQuery(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Query().Encode()
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func hmacSHA256(key []byte, data string) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write([]byte(data))
	return hash.Sum(nil)
}

func resolveModelVersion(requestModel string, info *relaycommon.RelayInfo) (string, error) {
	modelName := strings.TrimSpace(requestModel)
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	version, ok := modelVersionAliases[strings.ToLower(modelName)]
	if !ok {
		return "", fmt.Errorf("unsupported Tencent VOD Kling model %q", modelName)
	}
	return version, nil
}

func normalizeDuration(duration int, seconds string) (int, error) {
	if duration == 0 && strings.TrimSpace(seconds) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(seconds))
		if err != nil {
			return 0, errors.New("duration must be an integer number of seconds")
		}
		duration = parsed
	}
	if duration == 0 {
		duration = defaultDuration
	}
	if duration < 3 || duration > 15 {
		return 0, errors.New("Tencent VOD Kling duration must be between 3 and 15 seconds")
	}
	return duration, nil
}

func normalizeResolution(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "720P":
		return defaultResolution, nil
	case "1080P":
		return "1080P", nil
	default:
		return "", errors.New("Tencent VOD Kling resolution must be 720P or 1080P")
	}
}

func normalizeAspectRatio(value string, useDefault bool) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "16:9", "9:16", "1:1":
		return value, nil
	case "":
		if useDefault {
			return defaultAspectRatio, nil
		}
		return "", nil
	default:
		return "", errors.New("Tencent VOD Kling aspect ratio must be 16:9, 9:16, or 1:1")
	}
}

func aspectRatioFromSize(size string) string {
	switch strings.TrimSpace(size) {
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	case "720x720", "1024x1024":
		return "1:1"
	default:
		return ""
	}
}

func normalizeStorageMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "Permanent") {
		return "Permanent"
	}
	return defaultStorageMode
}

func normalizeEnabled(value any) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "Enabled"
		}
		return "Disabled"
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "enabled", "true", "1", "yes":
			return "Enabled"
		case "disabled", "false", "0", "no":
			return "Disabled"
		}
	}
	return ""
}

func buildFileInputs(req *relaycommon.TaskSubmitReq, metadata requestMetadata) ([]inputFileInfo, string, error) {
	if len(metadata.FileInfos) > 0 {
		return metadata.FileInfos, firstNonEmpty(metadata.LastFrameURL, metadata.ImageTail), nil
	}
	images := make([]string, 0, len(req.Images)+1)
	images = append(images, req.Images...)
	if strings.TrimSpace(req.InputReference) != "" {
		images = append(images, req.InputReference)
	}
	usage := normalizeImageUsage(metadata.ImageUsage)
	lastFrameURL := firstNonEmpty(metadata.LastFrameURL, metadata.ImageTail)
	if usage != "Reference" && lastFrameURL == "" && len(images) == 2 {
		lastFrameURL = images[1]
		images = images[:1]
	}
	if usage != "Reference" && len(images) > 1 {
		return nil, "", errors.New("Tencent VOD first-frame mode accepts one image; use metadata.image_usage=Reference for reference images")
	}
	fileInfos := make([]inputFileInfo, 0, len(images))
	for _, image := range images {
		fileInfo, err := parseImageInput(image, usage)
		if err != nil {
			return nil, "", err
		}
		fileInfos = append(fileInfos, fileInfo)
	}
	return fileInfos, lastFrameURL, nil
}

func normalizeImageUsage(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "Reference") {
		return "Reference"
	}
	return "FirstFrame"
}

func parseImageInput(value, usage string) (inputFileInfo, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return inputFileInfo{}, errors.New("image input cannot be empty")
	}
	fileInfo := inputFileInfo{Category: "Image", Usage: usage}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		fileInfo.Type = "Url"
		fileInfo.URL = value
		return fileInfo, nil
	}
	if strings.HasPrefix(value, "data:") {
		comma := strings.Index(value, ",")
		if comma < 0 || comma == len(value)-1 {
			return inputFileInfo{}, errors.New("invalid data URL image input")
		}
		value = value[comma+1:]
	}
	fileInfo.Type = "Base64"
	fileInfo.Base64 = value
	return fileInfo, nil
}

func taskFailed(task *aigcVideoTask, outerStatus string) bool {
	if task == nil {
		return true
	}
	errCodeExt := strings.TrimSpace(task.ErrCodeExt)
	hasExtendedError := errCodeExt != "" && errCodeExt != "0" && !strings.EqualFold(errCodeExt, "success")
	return task.ErrCode != 0 || hasExtendedError || strings.EqualFold(outerStatus, "ABORTED")
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func normalizedProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func firstOutputURL(files []outputFileInfo) string {
	for _, file := range files {
		if strings.TrimSpace(file.FileURL) != "" {
			return strings.TrimSpace(file.FileURL)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
