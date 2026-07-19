package seventhframe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

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
	ChannelName    = "seventh-frame"
	DefaultBaseURL = "https://diqizhen.jytt4.cn/api/v1"

	filesPath            = "/files"
	videoGenerationsPath = "/video/generations"
	upstreamChannel      = "channel14"
)

var ModelList = []string{
	"viraldance900--person-stripe--62ecbdc5--voice-tone--bcf91631",
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
	proxy   string
}

type generationRequest struct {
	Channel     string            `json:"channel"`
	Model       string            `json:"model,omitempty"`
	Prompt      string            `json:"prompt"`
	Duration    *int              `json:"duration,omitempty"`
	AspectRatio string            `json:"aspectRatio,omitempty"`
	Resolution  string            `json:"resolution,omitempty"`
	Seed        any               `json:"seed,omitempty"`
	Assets      []json.RawMessage `json:"assets,omitempty"`
}

type generationResponse struct {
	Generation upstreamGeneration `json:"generation"`
	Error      upstreamError      `json:"error"`
}

type upstreamGeneration struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Progress       int    `json:"progress"`
	OutputVideoURL string `json:"outputVideoUrl"`
	ErrorMessage   string `json:"errorMessage"`
}

type upstreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type assetReference struct {
	Type string
	URL  string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
	a.proxy = info.ChannelSetting.Proxy
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}
	mergeRequestOptions(c, &req)
	if strings.TrimSpace(req.Model) == "" {
		req.Model = info.OriginModelName
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if req.Duration != 0 && (req.Duration < 4 || req.Duration > 15) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 4 and 15 seconds"), "invalid_request", http.StatusBadRequest)
	}

	aspectRatio := requestAspectRatio(req)
	if aspectRatio != "" && !isSupportedAspectRatio(aspectRatio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("aspect_ratio must be one of 16:9, 9:16, 1:1, 4:3, or 3:4"), "invalid_request", http.StatusBadRequest)
	}
	if req.Resolution != "" && req.Resolution != "720p" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("resolution must be 720p"), "invalid_request", http.StatusBadRequest)
	}

	assets := collectAssetReferences(req)
	if countAssetsByType(assets, "image") > 9 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("at most 9 image assets are supported"), "invalid_request", http.StatusBadRequest)
	}
	if countAssetsByType(assets, "audio") > 3 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("at most 3 audio assets are supported"), "invalid_request", http.StatusBadRequest)
	}
	if countAssetsByType(assets, "video") > 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("video assets are not supported"), "invalid_request", http.StatusBadRequest)
	}

	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + videoGenerationsPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	payload, err := a.buildGenerationRequest(c.Request.Context(), req, info)
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

	var parsed generationResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", body), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if parsed.Error.Message != "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("Seventh Frame submit failed: %s", parsed.Error.Message), "submit_failed", http.StatusBadRequest)
	}
	if strings.TrimSpace(parsed.Generation.ID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("Seventh Frame submit returned an empty generation id"), "submit_failed", http.StatusBadRequest)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	video.Status = mapGenerationStatus(parsed.Generation.Status).ToVideoStatus()
	video.Progress = parsed.Generation.Progress
	c.JSON(http.StatusOK, video)
	return parsed.Generation.ID, body, nil
}

func (a *TaskAdaptor) FetchTask(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	uri := baseURL + videoGenerationsPath + "/" + url.PathEscape(taskID)
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
	var parsed generationResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	if parsed.Error.Message != "" {
		return &relaycommon.TaskInfo{
			Code:     1,
			Status:   string(model.TaskStatusFailure),
			Progress: taskcommon.ProgressComplete,
			Reason:   parsed.Error.Message,
		}, nil
	}
	status := mapGenerationStatus(parsed.Generation.Status)
	if status == model.TaskStatusUnknown {
		return nil, fmt.Errorf("unknown Seventh Frame task status %q", parsed.Generation.Status)
	}
	result := &relaycommon.TaskInfo{
		Code:     0,
		TaskID:   parsed.Generation.ID,
		Status:   string(status),
		Progress: generationProgress(parsed.Generation.Progress, status),
	}
	if status == model.TaskStatusSuccess {
		result.Url = strings.TrimSpace(parsed.Generation.OutputVideoURL)
		result.Progress = taskcommon.ProgressComplete
	}
	if status == model.TaskStatusFailure {
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(parsed.Generation.ErrorMessage, "task failed")
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) BuildPrivateData(_ *gin.Context, info *relaycommon.RelayInfo) (*model.TaskPrivateData, error) {
	if info == nil {
		return nil, fmt.Errorf("relay info is nil")
	}
	return &model.TaskPrivateData{Key: info.ApiKey}, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	if resultURL := strings.TrimSpace(task.GetResultURL()); resultURL != "" {
		video.SetMetadata("url", resultURL)
		video.SetMetadata("video_url", resultURL)
		video.SetMetadata("output_video_url", resultURL)
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) buildGenerationRequest(ctx context.Context, req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*generationRequest, error) {
	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = req.Model
	}
	payload := &generationRequest{
		Channel:     upstreamChannel,
		Model:       modelName,
		Prompt:      req.Prompt,
		AspectRatio: requestAspectRatio(req),
		Resolution:  req.Resolution,
		Seed:        requestSeed(req.Metadata),
	}
	if req.Duration > 0 {
		payload.Duration = &req.Duration
	}
	for _, asset := range collectAssetReferences(req) {
		file, err := a.uploadAsset(ctx, asset)
		if err != nil {
			return nil, err
		}
		payload.Assets = append(payload.Assets, file)
	}
	return payload, nil
}

func (a *TaskAdaptor) uploadAsset(ctx context.Context, asset assetReference) (json.RawMessage, error) {
	contents, contentType, filename, err := downloadAsset(ctx, asset.URL)
	if err != nil {
		return nil, fmt.Errorf("download %s asset failed: %w", asset.Type, err)
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": filename,
	}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("create upload form failed: %w", err)
	}
	if _, err := part.Write(contents); err != nil {
		return nil, fmt.Errorf("write upload form failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close upload form failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+filesPath, &form)
	if err != nil {
		return nil, fmt.Errorf("create upload request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	client, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload asset failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upload response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("upload asset failed with status %d", resp.StatusCode)
	}

	var response struct {
		File json.RawMessage `json:"file"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode upload response failed: %w", err)
	}
	if common.GetJsonType(response.File) != "object" {
		return nil, fmt.Errorf("upload response missing file object")
	}
	return response.File, nil
}

func downloadAsset(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	resp, err := service.DoDownloadRequestWithContext(ctx, rawURL, "seventh_frame_asset")
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	maxBytes := int64(constant.MaxFileDownloadMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 64 * 1024 * 1024
	}
	if contentLength := resp.ContentLength; contentLength > maxBytes {
		return nil, "", "", fmt.Errorf("file size exceeds maximum allowed size")
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read asset failed: %w", err)
	}
	if int64(len(contents)) > maxBytes {
		return nil, "", "", fmt.Errorf("file size exceeds maximum allowed size")
	}

	contentType := mediaType(resp.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(contents)
	}
	return contents, contentType, assetFilename(rawURL, resp.Header.Get("Content-Disposition"), contentType), nil
}

func mergeRequestOptions(c *gin.Context, req *relaycommon.TaskSubmitReq) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		return
	}
	var raw map[string]any
	if err := common.Unmarshal(body, &raw); err != nil {
		return
	}
	if aspectRatio, ok := raw["aspectRatio"].(string); ok && strings.TrimSpace(aspectRatio) != "" {
		req.AspectRatio = strings.TrimSpace(aspectRatio)
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	if seed, ok := raw["seed"]; ok {
		req.Metadata["seed"] = seed
	}
}

func collectAssetReferences(req relaycommon.TaskSubmitReq) []assetReference {
	assets := make([]assetReference, 0, len(req.Images)+len(req.Audios)+len(req.Videos)+len(req.Content))
	seen := make(map[string]struct{})
	appendURLs := func(assetType string, urls ...[]string) {
		for _, values := range urls {
			for _, value := range values {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				key := assetType + "\x00" + value
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				assets = append(assets, assetReference{Type: assetType, URL: value})
			}
		}
	}
	appendURLs("image", req.Images, req.ImageURLs, req.InputStartFrames, req.InputImageReferences, req.MetadataStartFrames, []string{req.Image, req.InputReference})
	appendURLs("video", req.Videos, req.VideoURLs)
	appendURLs("audio", req.Audios, req.AudioURLs)
	for _, item := range req.Content {
		if item.ImageURL != nil {
			appendURLs("image", []string{item.ImageURL.URL})
		}
		if item.VideoURL != nil {
			appendURLs("video", []string{item.VideoURL.URL})
		}
		if item.AudioURL != nil {
			appendURLs("audio", []string{item.AudioURL.URL})
		}
	}
	return assets
}

func requestAspectRatio(req relaycommon.TaskSubmitReq) string {
	if aspectRatio := strings.TrimSpace(req.AspectRatio); aspectRatio != "" {
		return aspectRatio
	}
	if value, ok := req.Metadata["aspect_ratio"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return aspectRatioFromSize(req.Size)
}

func requestSeed(metadata map[string]any) any {
	if metadata == nil {
		return nil
	}
	return metadata["seed"]
}

func countAssetsByType(assets []assetReference, assetType string) int {
	count := 0
	for _, asset := range assets {
		if asset.Type == assetType {
			count++
		}
	}
	return count
}

func mapGenerationStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued":
		return model.TaskStatusQueued
	case "running":
		return model.TaskStatusInProgress
	case "succeeded":
		return model.TaskStatusSuccess
	case "failed", "blocked":
		return model.TaskStatusFailure
	default:
		return model.TaskStatusUnknown
	}
}

func generationProgress(progress int, status model.TaskStatus) string {
	if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
		return taskcommon.ProgressComplete
	}
	if progress > 0 {
		return strconv.Itoa(progress) + "%"
	}
	if status == model.TaskStatusQueued {
		return taskcommon.ProgressQueued
	}
	return taskcommon.ProgressInProgress
}

func isSupportedAspectRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "1:1", "4:3", "3:4":
		return true
	default:
		return false
	}
}

func aspectRatioFromSize(size string) string {
	switch strings.TrimSpace(size) {
	case "1280x720", "1792x1024":
		return "16:9"
	case "720x1280", "1024x1792":
		return "9:16"
	case "720x720", "1024x1024":
		return "1:1"
	case "1024x768":
		return "4:3"
	case "768x1024":
		return "3:4"
	default:
		return ""
	}
}

func mediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return mediaType
}

func assetFilename(rawURL, contentDisposition, contentType string) string {
	if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
		if filename := sanitizeFilename(params["filename"]); filename != "" {
			return filename
		}
	}
	if parsedURL, err := url.Parse(rawURL); err == nil {
		if filename := sanitizeFilename(path.Base(parsedURL.Path)); filename != "" && filename != "." {
			return filename
		}
	}
	if extensions, err := mime.ExtensionsByType(contentType); err == nil && len(extensions) > 0 {
		return "asset" + extensions[0]
	}
	return "asset"
}

func sanitizeFilename(filename string) string {
	filename = path.Base(strings.TrimSpace(filename))
	filename = strings.ReplaceAll(filename, "\x00", "")
	if filename == "." || filename == "/" {
		return ""
	}
	return filename
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
