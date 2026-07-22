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
)

var ModelList = []string{
	"viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78",
	"seedance-2.0--person-stripe--6e9f7f9c--voice-tone--a7f8bf20",
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
	proxy   string
	channel string
}

type generationRequest struct {
	Channel string `json:"channel"`
	Model   string `json:"model,omitempty"`
	Prompt  string `json:"prompt"`
	// Keep the original numeric representation so an explicit zero and a
	// fractional duration survive the unified request DTO's integer field.
	Duration    json.RawMessage   `json:"duration,omitempty"`
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
	baseURL, upstreamChannel, err := dto.ParseSeventhFrameBaseURL(info.ChannelBaseUrl)
	if err != nil {
		baseURL = ""
		upstreamChannel = dto.DefaultSeventhFrameChannel
	}
	a.baseURL = baseURL
	if a.baseURL == "" {
		a.baseURL = DefaultBaseURL
	}
	a.proxy = info.ChannelSetting.Proxy
	a.channel = upstreamChannel
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
	duration, err := seventhFrameDuration(c, req)
	if err != nil {
		return nil, err
	}
	payload, err := a.buildGenerationRequestWithDuration(c.Request.Context(), req, info, duration)
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
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("SeventhFrame submit failed: %s", parsed.Error.Message), "submit_failed", http.StatusBadRequest)
	}
	if strings.TrimSpace(parsed.Generation.ID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("SeventhFrame submit returned an empty generation id"), "submit_failed", http.StatusBadRequest)
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
	var err error
	baseURL, _, err = dto.ParseSeventhFrameBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("unknown SeventhFrame task status %q", parsed.Generation.Status)
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
	duration, err := taskSubmitDuration(req)
	if err != nil {
		return nil, err
	}
	return a.buildGenerationRequestWithDuration(ctx, req, info, duration)
}

func (a *TaskAdaptor) buildGenerationRequestWithDuration(ctx context.Context, req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo, duration json.RawMessage) (*generationRequest, error) {
	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = req.Model
	}
	payload := &generationRequest{
		Channel:     a.channel,
		Model:       modelName,
		Prompt:      req.Prompt,
		Duration:    duration,
		AspectRatio: requestAspectRatio(req),
		Resolution:  req.Resolution,
		Seed:        requestSeed(req.Metadata),
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
	header.Set("Content-Disposition", fmt.Sprintf(
		`form-data; name="file"; filename="%s"`,
		uploadFilename(filename, contentType),
	))
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
		return nil, fmt.Errorf("upload asset failed with status %d: %s", resp.StatusCode, truncateUploadError(responseBody))
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
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	if seed, ok := raw["seed"]; ok {
		req.Metadata["seed"] = seed
	}
}

func collectAssetReferences(req relaycommon.TaskSubmitReq) []assetReference {
	assets := make([]assetReference, 0, len(req.Images)+len(req.Audios)+len(req.Videos)+len(req.Content))
	appendSource := func(references []assetReference) {
		for _, reference := range references {
			reference.URL = strings.TrimSpace(reference.URL)
			if reference.URL == "" {
				continue
			}
			assets = append(assets, reference)
		}
	}
	appendURLs := func(assetType string, urls []string) {
		references := make([]assetReference, 0, len(urls))
		for _, value := range urls {
			references = append(references, assetReference{Type: assetType, URL: value})
		}
		appendSource(references)
	}

	appendURLs("image", req.Images)
	appendURLs("image", req.ImageURLs)
	appendURLs("image", req.InputStartFrames)
	appendURLs("image", req.InputImageReferences)
	appendURLs("image", req.MetadataStartFrames)
	appendURLs("image", []string{req.Image})
	appendURLs("image", []string{req.InputReference})
	appendURLs("video", req.Videos)
	appendURLs("video", req.VideoURLs)
	appendURLs("audio", req.Audios)
	appendURLs("audio", req.AudioURLs)

	contentAssets := make([]assetReference, 0, len(req.Content))
	for _, item := range req.Content {
		if item.ImageURL != nil {
			contentAssets = append(contentAssets, assetReference{Type: "image", URL: item.ImageURL.URL})
		}
		if item.VideoURL != nil {
			contentAssets = append(contentAssets, assetReference{Type: "video", URL: item.VideoURL.URL})
		}
		if item.AudioURL != nil {
			contentAssets = append(contentAssets, assetReference{Type: "audio", URL: item.AudioURL.URL})
		}
	}
	appendSource(contentAssets)
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

func seventhFrameDuration(c *gin.Context, req relaycommon.TaskSubmitReq) (json.RawMessage, error) {
	contentType := c.GetHeader("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, err
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, err
		}

		var fields map[string]json.RawMessage
		if err := common.Unmarshal(body, &fields); err != nil {
			return nil, err
		}
		if duration, ok := fields["duration"]; ok {
			return upstreamDuration(duration), nil
		}
		if seconds, ok := fields["seconds"]; ok {
			return upstreamDuration(seconds), nil
		}
	case strings.Contains(contentType, gin.MIMEMultipartPOSTForm):
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		if duration, ok, err := formDuration(form.Value); ok || err != nil {
			return duration, err
		}
	case strings.Contains(contentType, gin.MIMEPOSTForm):
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, err
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		if duration, ok, err := formDuration(values); ok || err != nil {
			return duration, err
		}
	}

	return taskSubmitDuration(req)
}

func formDuration(values map[string][]string) (json.RawMessage, bool, error) {
	for _, field := range []string{"duration", "seconds"} {
		if fieldValues, ok := values[field]; ok && len(fieldValues) > 0 {
			raw, err := common.Marshal(fieldValues[0])
			if err != nil {
				return nil, true, err
			}
			return upstreamDuration(raw), true, nil
		}
	}
	return nil, false, nil
}

func taskSubmitDuration(req relaycommon.TaskSubmitReq) (json.RawMessage, error) {
	if req.Duration != 0 {
		return json.RawMessage(strconv.Itoa(req.Duration)), nil
	}
	if strings.TrimSpace(req.Seconds) == "" {
		return nil, nil
	}
	seconds, err := common.Marshal(req.Seconds)
	if err != nil {
		return nil, err
	}
	return upstreamDuration(seconds), nil
}

func upstreamDuration(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	var value string
	if len(raw) == 0 || common.Unmarshal(raw, &value) != nil {
		return raw
	}

	value = strings.TrimSpace(value)
	lowerValue := strings.ToLower(value)
	for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
		if strings.HasSuffix(lowerValue, suffix) {
			value = strings.TrimSpace(value[:len(value)-len(suffix)])
			break
		}
	}
	var number json.Number
	if value == "" || common.Unmarshal([]byte(value), &number) != nil {
		return raw
	}
	return json.RawMessage(value)
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

func uploadFilename(filename, contentType string) string {
	if isSafeUploadFilename(filename) {
		return filename
	}
	extension := strings.ToLower(path.Ext(filename))
	if !isSafeFileExtension(extension) {
		extension = ""
		if extensions, err := mime.ExtensionsByType(contentType); err == nil {
			for _, candidate := range extensions {
				candidate = strings.ToLower(candidate)
				if isSafeFileExtension(candidate) {
					extension = candidate
					break
				}
			}
		}
	}
	if extension == "" {
		extension = ".bin"
	}
	return "asset" + extension
}

func isSafeUploadFilename(filename string) bool {
	if filename == "" || filename == "." || filename == ".." || len(filename) > 128 {
		return false
	}
	for _, character := range filename {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func isSafeFileExtension(extension string) bool {
	if len(extension) < 2 || len(extension) > 16 || extension[0] != '.' {
		return false
	}
	for _, character := range extension[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func truncateUploadError(body []byte) string {
	const maxLength = 512
	message := strings.TrimSpace(string(body))
	if message == "" {
		return "empty response body"
	}
	if len(message) > maxLength {
		return message[:maxLength] + "..."
	}
	return message
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
