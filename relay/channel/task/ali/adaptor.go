package ali

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// AliVideoRequest 阿里通义万相视频生成请求
type AliVideoRequest struct {
	Model      string              `json:"model"`
	Input      AliVideoInput       `json:"input"`
	Parameters *AliVideoParameters `json:"parameters,omitempty"`
}

// AliVideoInput 视频输入参数
type AliVideoInput struct {
	Prompt         string `json:"prompt,omitempty"`          // 文本提示词
	ImgURL         string `json:"img_url,omitempty"`         // 首帧图像URL或Base64（图生视频）
	FirstFrameURL  string `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	AudioURL       string `json:"audio_url,omitempty"`       // 音频URL（wan2.5支持）
	NegativePrompt string `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string `json:"template,omitempty"`        // 视频特效模板
}

// AliVideoParameters 视频参数
type AliVideoParameters struct {
	Resolution   string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P（图生视频、首尾帧生视频）
	Size         string `json:"size,omitempty"`          // 尺寸: 如 "832*480"（文生视频）
	Duration     int    `json:"duration,omitempty"`      // 时长: 3-10秒
	PromptExtend bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool  `json:"audio,omitempty"`         // 是否添加音频（wan2.5）
	Seed         int    `json:"seed,omitempty"`          // 随机数种子
}

// AliVideoResponse 阿里通义万相响应
type AliVideoResponse struct {
	Output    AliVideoOutput `json:"output"`
	RequestID string         `json:"request_id"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	Usage     *AliUsage      `json:"usage,omitempty"`
}

// AliVideoOutput 输出信息
type AliVideoOutput struct {
	TaskID        string `json:"task_id"`
	TaskStatus    string `json:"task_status"`
	SubmitTime    string `json:"submit_time,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	OrigPrompt    string `json:"orig_prompt,omitempty"`
	ActualPrompt  string `json:"actual_prompt,omitempty"`
	VideoURL      string `json:"video_url,omitempty"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// AliUsage 使用统计
type AliUsage struct {
	Duration   dto.IntValue `json:"duration,omitempty"`
	VideoCount dto.IntValue `json:"video_count,omitempty"`
	SR         dto.IntValue `json:"SR,omitempty"`
}

type AliMetadata struct {
	// Input 相关
	AudioURL       string `json:"audio_url,omitempty"`       // 音频URL
	ImgURL         string `json:"img_url,omitempty"`         // 图片URL（图生视频）
	FirstFrameURL  string `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
	LastFrameURL   string `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
	NegativePrompt string `json:"negative_prompt,omitempty"` // 反向提示词
	Template       string `json:"template,omitempty"`        // 视频特效模板

	// Parameters 相关
	Resolution   *string `json:"resolution,omitempty"`    // 分辨率: 480P/720P/1080P
	Size         *string `json:"size,omitempty"`          // 尺寸: 如 "832*480"
	Duration     *int    `json:"duration,omitempty"`      // 时长
	PromptExtend *bool   `json:"prompt_extend,omitempty"` // 是否开启prompt智能改写
	Watermark    *bool   `json:"watermark,omitempty"`     // 是否添加水印
	Audio        *bool   `json:"audio,omitempty"`         // 是否添加音频
	Seed         *int    `json:"seed,omitempty"`          // 随机数种子
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

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// ValidateMultipartDirect 负责解析并将原始 TaskSubmitReq 存入 context
	return relaycommon.ValidateMultipartDirect(c, info)
}

// ValidateMappedRequest validates media against the final Ali model before
// billing. The DashScope video API has scalar image/audio inputs and no video
// reference input, so unsupported common media must not be silently omitted.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if err := validateAliStandardMedia(&req, aliRequestModelName(req, info)); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, err := a.convertToAliRequest(info, req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_output", http.StatusBadRequest)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

// BuildRequestHeader sets required headers for Ali API
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable") // 阿里异步任务必须设置
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "convert_to_ali_request_failed")
	}
	logger.LogJson(c, "ali video request body", aliReq)

	bodyBytes, err := common.Marshal(aliReq)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_ali_request_failed")
	}
	return bytes.NewReader(bodyBytes), nil
}

var (
	size480p = []string{
		"832*480",
		"480*832",
		"624*624",
	}
	size720p = []string{
		"1280*720",
		"720*1280",
		"960*960",
		"1088*832",
		"832*1088",
	}
	size1080p = []string{
		"1920*1080",
		"1080*1920",
		"1440*1440",
		"1632*1248",
		"1248*1632",
	}
)

func sizeToResolution(size string) (string, error) {
	if lo.Contains(size480p, size) {
		return "480P", nil
	} else if lo.Contains(size720p, size) {
		return "720P", nil
	} else if lo.Contains(size1080p, size) {
		return "1080P", nil
	}
	return "", fmt.Errorf("invalid size: %s", size)
}

func ProcessAliOtherRatios(aliReq *AliVideoRequest) (map[string]float64, error) {
	otherRatios := make(map[string]float64)
	aliRatios := map[string]map[string]float64{
		"wan2.6-i2v": {
			"720P":  1,
			"1080P": 1 / 0.6,
		},
		"wan2.5-t2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-t2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.5-i2v-preview": {
			"480P":  1,
			"720P":  2,
			"1080P": 1 / 0.3,
		},
		"wan2.2-i2v-plus": {
			"480P":  1,
			"1080P": 0.7 / 0.14,
		},
		"wan2.2-kf2v-flash": {
			"480P":  1,
			"720P":  2,
			"1080P": 4.8,
		},
		"wan2.2-i2v-flash": {
			"480P": 1,
			"720P": 2,
		},
		"wan2.2-s2v": {
			"480P": 1,
			"720P": 0.9 / 0.5,
		},
	}
	var resolution string

	// size match
	if aliReq.Parameters.Size != "" {
		toResolution, err := sizeToResolution(aliReq.Parameters.Size)
		if err != nil {
			return nil, err
		}
		resolution = toResolution
	} else {
		resolution = strings.ToUpper(aliReq.Parameters.Resolution)
		if !strings.HasSuffix(resolution, "P") {
			resolution = resolution + "P"
		}
	}
	if otherRatio, ok := aliRatios[aliReq.Model]; ok {
		if ratio, ok := otherRatio[resolution]; ok {
			otherRatios[fmt.Sprintf("resolution-%s", resolution)] = ratio
		}
	}
	return otherRatios, nil
}

func (a *TaskAdaptor) convertToAliRequest(info *relaycommon.RelayInfo, req relaycommon.TaskSubmitReq) (*AliVideoRequest, error) {
	upstreamModel := aliRequestModelName(req, info)
	aliReq := &AliVideoRequest{
		Model: upstreamModel,
		Input: AliVideoInput{
			Prompt: req.Prompt,
		},
		Parameters: &AliVideoParameters{
			PromptExtend: true, // 默认开启智能改写
			Watermark:    false,
		},
	}

	// Defaults are only used when neither a canonical quality tier nor a
	// legacy provider-specific size was supplied. Metadata remains a fallback
	// and is merged below; explicit public fields are reapplied after that merge.
	if req.Size == "" && req.Resolution == "" {
		// 根据模型设置默认分辨率
		if isAliTextToVideoModel(upstreamModel) {
			if strings.HasPrefix(upstreamModel, "wan2.5") {
				aliReq.Parameters.Size = "1920*1080"
			} else if strings.HasPrefix(upstreamModel, "wan2.2") {
				aliReq.Parameters.Size = "1920*1080"
			} else {
				aliReq.Parameters.Size = "1280*720"
			}
		} else {
			if strings.HasPrefix(upstreamModel, "wan2.6") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(upstreamModel, "wan2.5") {
				aliReq.Parameters.Resolution = "1080P"
			} else if strings.HasPrefix(upstreamModel, "wan2.2-i2v-flash") {
				aliReq.Parameters.Resolution = "720P"
			} else if strings.HasPrefix(upstreamModel, "wan2.2-i2v-plus") {
				aliReq.Parameters.Resolution = "1080P"
			} else {
				aliReq.Parameters.Resolution = "720P"
			}
		}
	}

	// 处理时长
	if req.Duration > 0 {
		aliReq.Parameters.Duration = req.Duration
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, errors.Wrap(err, "convert seconds to int failed")
		} else {
			aliReq.Parameters.Duration = seconds
		}
	} else {
		aliReq.Parameters.Duration = 5 // 默认5秒
	}

	// 从 metadata 中提取额外参数
	if req.Metadata != nil {
		if metadataBytes, err := common.Marshal(req.Metadata); err == nil {
			err = common.Unmarshal(metadataBytes, aliReq)
			if err != nil {
				return nil, errors.Wrap(err, "unmarshal metadata failed")
			}
		} else {
			return nil, errors.Wrap(err, "marshal metadata failed")
		}
	}

	if aliReq.Model != upstreamModel {
		return nil, errors.New("can't change model with metadata")
	}
	if aliReq.Parameters == nil {
		return nil, errors.New("metadata must not clear parameters")
	}
	if err := applyAliStandardMedia(&aliReq.Input, req, upstreamModel); err != nil {
		return nil, err
	}
	if err := applyAliCanonicalVideoOutput(aliReq.Parameters, req, upstreamModel); err != nil {
		return nil, err
	}

	return aliReq, nil
}

func aliRequestModelName(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil && info.ChannelMeta != nil && info.IsModelMapped {
		if upstreamModel := strings.TrimSpace(info.UpstreamModelName); upstreamModel != "" {
			return upstreamModel
		}
	}
	return strings.TrimSpace(req.Model)
}

func applyAliStandardMedia(input *AliVideoInput, req relaycommon.TaskSubmitReq, model string) error {
	if input == nil {
		return errors.New("Ali input is required")
	}
	if err := validateAliStandardMedia(&req, model); err != nil {
		return err
	}

	images := aliRequestImageURLs(&req)
	if len(images) > 0 {
		if isAliKeyFrameVideoModel(model) {
			input.ImgURL = ""
			input.FirstFrameURL = images[0]
			if len(images) == 2 {
				input.LastFrameURL = images[1]
			}
		} else {
			input.ImgURL = images[0]
		}
	}

	audios := aliRequestAudioURLs(&req)
	if len(audios) > 0 {
		input.AudioURL = audios[0]
	}
	return nil
}

func validateAliStandardMedia(req *relaycommon.TaskSubmitReq, model string) error {
	if req == nil {
		return nil
	}

	if videos := aliRequestVideoURLs(req); len(videos) > 0 {
		return errors.New("Ali Wan does not support video reference inputs")
	}

	images := aliRequestImageURLs(req)
	if len(images) > 0 && isAliTextToVideoModel(model) {
		return fmt.Errorf("Ali Wan text-to-video model %q does not support image inputs", model)
	}
	if len(images) > 2 {
		return fmt.Errorf("Ali Wan supports at most two image inputs, got %d", len(images))
	}
	if len(images) > 1 && !isAliKeyFrameVideoModel(model) {
		return fmt.Errorf("Ali Wan model %q supports only one image input; use a keyframe-to-video model for two images", model)
	}

	audios := aliRequestAudioURLs(req)
	if len(audios) > 1 {
		return fmt.Errorf("Ali Wan supports at most one audio input, got %d", len(audios))
	}
	if len(audios) > 0 && !isAliAudioReferenceModel(model) {
		return fmt.Errorf("Ali Wan model %q does not support audio reference inputs", model)
	}
	return nil
}

func aliMediaURLs(groups ...[]string) []string {
	urls := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			if value = strings.TrimSpace(value); value != "" {
				urls = append(urls, value)
			}
		}
	}
	return urls
}

func aliRequestImageURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	contentImages, _, _ := aliContentMediaURLs(req.Content)
	return aliMediaURLs(
		req.Images,
		[]string{req.Image},
		req.ImageURLs,
		[]string{req.InputReference},
		req.InputStartFrames,
		req.InputImageReferences,
		req.MetadataStartFrames,
		contentImages,
	)
}

func aliRequestVideoURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	_, contentVideos, _ := aliContentMediaURLs(req.Content)
	return aliMediaURLs(req.Videos, req.VideoURLs, contentVideos)
}

func aliRequestAudioURLs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}
	_, _, contentAudios := aliContentMediaURLs(req.Content)
	return aliMediaURLs(req.Audios, req.AudioURLs, contentAudios)
}

func aliContentMediaURLs(content []relaycommon.TaskContentItem) (images, videos, audios []string) {
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
	return aliMediaURLs(images), aliMediaURLs(videos), aliMediaURLs(audios)
}

func isAliKeyFrameVideoModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "kf2v")
}

func isAliAudioReferenceModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "wan2.5-i2v")
}

func isAliTextToVideoModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "t2v")
}

// applyAliCanonicalVideoOutput gives the public resolution field precedence
// over the legacy size field. Ali's text-to-video API requires a concrete
// W*H size, so only its documented quality and aspect-ratio combinations are
// converted. Unknown pixel sizes must not be mistaken for a quality tier.
func applyAliCanonicalVideoOutput(parameters *AliVideoParameters, req relaycommon.TaskSubmitReq, model string) error {
	if parameters == nil {
		return errors.New("Ali parameters are required")
	}
	if err := validateAliLegacySizeOutput(req.Size, req.AspectRatio); err != nil {
		return err
	}
	if req.Resolution != "" {
		if isAliTextToVideoModel(model) {
			size, err := aliTextToVideoSize(req.AspectRatio, req.Resolution)
			if err != nil {
				return err
			}
			parameters.Size = size
			parameters.Resolution = ""
			return validateAliEffectiveVideoOutput(parameters)
		}
		resolution, err := aliResolutionLabel(req.Resolution)
		if err != nil {
			return err
		}
		parameters.Size = ""
		parameters.Resolution = resolution
		return validateAliEffectiveVideoOutput(parameters)
	}
	if isAliTextToVideoModel(model) && req.AspectRatio != "" && req.Size == "" {
		resolution, err := aliTextToVideoEffectiveResolution(parameters)
		if err != nil {
			return err
		}
		size, err := aliTextToVideoSize(req.AspectRatio, resolution)
		if err != nil {
			return err
		}
		parameters.Size = size
		parameters.Resolution = ""
		return validateAliEffectiveVideoOutput(parameters)
	}
	if req.Size == "" {
		return validateAliEffectiveVideoOutput(parameters)
	}
	if isAliTextToVideoModel(model) {
		if !strings.Contains(req.Size, "*") {
			return fmt.Errorf("invalid size: %s, example: %s", req.Size, "1920*1080")
		}
		parameters.Size = req.Size
		parameters.Resolution = ""
		return validateAliEffectiveVideoOutput(parameters)
	}
	if strings.Contains(req.Size, "*") {
		parameters.Size = req.Size
		parameters.Resolution = ""
		return validateAliEffectiveVideoOutput(parameters)
	}
	resolution, err := aliResolutionLabel(req.Size)
	if err != nil {
		return err
	}
	parameters.Size = ""
	parameters.Resolution = resolution
	return validateAliEffectiveVideoOutput(parameters)
}

// validateAliLegacySizeOutput checks DashScope's provider-specific W*H sizes
// before billing. Unlike public WxH sizes, this syntax cannot be interpreted
// globally, so it is validated only after the Ali model is known.
func validateAliLegacySizeOutput(size, aspectRatio string) error {
	size = strings.TrimSpace(size)
	if size == "" || !strings.Contains(size, "*") {
		return nil
	}
	if _, err := sizeToResolution(size); err != nil {
		return err
	}
	if aspectRatio == "" {
		return nil
	}
	actual, err := aliLegacySizeAspectRatio(size)
	if err != nil {
		return err
	}
	if aspectRatio == "adaptive" || actual != aspectRatio {
		return fmt.Errorf("size %q conflicts with aspect_ratio %q", size, aspectRatio)
	}
	return nil
}

func aliLegacySizeAspectRatio(size string) (string, error) {
	parts := strings.Split(strings.TrimSpace(size), "*")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid size: %s", size)
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid size: %s", size)
	}
	divisor := aliGreatestCommonDivisor(width, height)
	return strconv.Itoa(width/divisor) + ":" + strconv.Itoa(height/divisor), nil
}

func aliGreatestCommonDivisor(left, right int) int {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}

func validateAliEffectiveVideoOutput(parameters *AliVideoParameters) error {
	if parameters == nil {
		return errors.New("Ali parameters are required")
	}
	if size := strings.TrimSpace(parameters.Size); size != "" {
		if _, err := sizeToResolution(size); err != nil {
			return err
		}
	}
	if resolution := strings.TrimSpace(parameters.Resolution); resolution != "" {
		if _, err := aliResolutionLabel(resolution); err != nil {
			return err
		}
	}
	return nil
}

func aliTextToVideoEffectiveResolution(parameters *AliVideoParameters) (string, error) {
	if parameters == nil {
		return "", errors.New("Ali parameters are required")
	}
	if size := strings.TrimSpace(parameters.Size); size != "" {
		resolution, err := sizeToResolution(size)
		if err != nil {
			return "", fmt.Errorf("cannot apply aspect_ratio to Ali text-to-video size %q: %w", size, err)
		}
		return resolution, nil
	}
	if resolution := strings.TrimSpace(parameters.Resolution); resolution != "" {
		return aliResolutionLabel(resolution)
	}
	return "", errors.New("Ali text-to-video requires a resolution before applying aspect_ratio")
}

func aliResolutionLabel(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "480p":
		return "480P", nil
	case "720p":
		return "720P", nil
	case "1080p":
		return "1080P", nil
	default:
		return "", fmt.Errorf("unsupported Ali resolution %q; supported values are 480p, 720p, 1080p", value)
	}
}

func aliTextToVideoSize(aspectRatio, resolution string) (string, error) {
	resolution, err := aliResolutionLabel(resolution)
	if err != nil {
		return "", err
	}
	aspectRatio = strings.TrimSpace(aspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	sizes := map[string]map[string]string{
		"16:9": {
			"480P":  "832*480",
			"720P":  "1280*720",
			"1080P": "1920*1080",
		},
		"9:16": {
			"480P":  "480*832",
			"720P":  "720*1280",
			"1080P": "1080*1920",
		},
		"1:1": {
			"480P":  "624*624",
			"720P":  "960*960",
			"1080P": "1440*1440",
		},
	}
	if size, ok := sizes[aspectRatio][resolution]; ok {
		return size, nil
	}
	return "", fmt.Errorf("unsupported Ali text-to-video aspect_ratio %q for resolution %s", aspectRatio, strings.ToLower(resolution))
}

// EstimateBilling 根据用户请求参数计算 OtherRatios（时长、分辨率等）。
// 在 ValidateRequestAndSetAction 之后、价格计算之前调用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	aliReq, err := a.convertToAliRequest(info, taskReq)
	if err != nil {
		return nil
	}

	otherRatios := map[string]float64{
		"seconds": float64(aliReq.Parameters.Duration),
	}
	ratios, err := ProcessAliOtherRatios(aliReq)
	if err != nil {
		return otherRatios
	}
	for k, v := range ratios {
		otherRatios[k] = v
	}
	return otherRatios
}

// DoRequest delegates to common helper
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// 解析阿里响应
	var aliResp AliVideoResponse
	if err := common.Unmarshal(responseBody, &aliResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 检查错误
	if aliResp.Code != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", aliResp.Code, aliResp.Message), "ali_api_error", resp.StatusCode)
		return
	}

	if aliResp.Output.TaskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 转换为 OpenAI 格式响应
	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = info.PublicTaskID
	openAIResp.TaskID = info.PublicTaskID
	openAIResp.Model = c.GetString("model")
	if openAIResp.Model == "" && info != nil {
		openAIResp.Model = info.OriginModelName
	}
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.CreatedAt = common.GetTimestamp()

	// 返回 OpenAI 格式
	c.JSON(http.StatusOK, openAIResp)

	return aliResp.Output.TaskID, responseBody, nil
}

// FetchTask 查询任务状态
func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)

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

// ParseTaskResult 解析任务结果
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(respBody, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// 状态映射
	switch aliResp.Output.TaskStatus {
	case "PENDING":
		taskResult.Status = model.TaskStatusQueued
	case "RUNNING":
		taskResult.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskResult.Status = model.TaskStatusSuccess
		// 阿里直接返回视频URL，不需要额外的代理端点
		taskResult.Url = aliResp.Output.VideoURL
	case "FAILED", "CANCELED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		if aliResp.Message != "" {
			taskResult.Reason = aliResp.Message
		} else if aliResp.Output.Message != "" {
			taskResult.Reason = fmt.Sprintf("task failed, code: %s , message: %s", aliResp.Output.Code, aliResp.Output.Message)
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		return nil, fmt.Errorf("unknown Ali task status %q", aliResp.Output.TaskStatus)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var aliResp AliVideoResponse
	if err := common.Unmarshal(task.Data, &aliResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal ali response failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertAliStatus(aliResp.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.CompletionTime()

	// 设置视频URL（核心字段）
	openAIResp.SetMetadata("url", aliResp.Output.VideoURL)

	// 错误处理
	if aliResp.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Code,
			Message: aliResp.Message,
		}
	} else if aliResp.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    aliResp.Output.Code,
			Message: aliResp.Output.Message,
		}
	}

	return common.Marshal(openAIResp)
}

func convertAliStatus(aliStatus string) string {
	switch aliStatus {
	case "PENDING":
		return dto.VideoStatusQueued
	case "RUNNING":
		return dto.VideoStatusInProgress
	case "SUCCEEDED":
		return dto.VideoStatusCompleted
	case "FAILED", "CANCELED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}
