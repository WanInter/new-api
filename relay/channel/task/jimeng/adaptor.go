package jimeng

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// ============================
// Request / Response structures
// ============================

type requestPayload struct {
	ReqKey           string   `json:"req_key"`
	BinaryDataBase64 []string `json:"binary_data_base64,omitempty"`
	ImageUrls        []string `json:"image_urls,omitempty"`
	Prompt           string   `json:"prompt,omitempty"`
	Seed             int64    `json:"seed"`
	AspectRatio      string   `json:"aspect_ratio"`
	Frames           int      `json:"frames,omitempty"`
}

type responsePayload struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestId string `json:"request_id"`
	Data      struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

type responseTask struct {
	Code int `json:"code"`
	Data struct {
		BinaryDataBase64 []interface{} `json:"binary_data_base64"`
		ImageUrls        interface{}   `json:"image_urls"`
		RespData         string        `json:"resp_data"`
		Status           string        `json:"status"`
		VideoUrl         string        `json:"video_url"`
	} `json:"data"`
	Message     string `json:"message"`
	RequestId   string `json:"request_id"`
	Status      int    `json:"status"`
	TimeElapsed string `json:"time_elapsed"`
}

const (
	// 即梦限制单个文件最大4.7MB https://www.volcengine.com/docs/85621/1747301
	MaxFileSize int64 = 4*1024*1024 + 700*1024 // 4.7MB (4MB + 724KB)
)

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	accessKey   string
	secretKey   string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl

	// apiKey format: "access_key|secret_key"
	keyParts := strings.Split(info.ApiKey, "|")
	if len(keyParts) == 2 {
		a.accessKey = strings.TrimSpace(keyParts[0])
		a.secretKey = strings.TrimSpace(keyParts[1])
	}
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// ValidateMappedRequest runs after model mapping and before billing. Jimeng
// only accepts image references, represented upstream as either HTTP(S) URLs
// or base64 data, so unsupported media must not be silently discarded.
func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, _ *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "get_task_request_failed", http.StatusBadRequest)
	}
	if err := validateJimengMediaInputs(&req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_media_input", http.StatusBadRequest)
	}
	if err := validateJimengMultipartInputReferences(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_media_input", http.StatusBadRequest)
	}
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if isNewAPIRelay(info.ApiKey) {
		return fmt.Sprintf("%s/jimeng/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31", a.baseURL), nil
	}
	return fmt.Sprintf("%s/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if isNewAPIRelay(info.ApiKey) {
		req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	} else {
		return a.signRequest(req, a.accessKey, a.secretKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}
	// 支持openai sdk的图片上传方式
	if mf, err := c.MultipartForm(); err == nil {
		if files, exists := mf.File["input_reference"]; exists && len(files) > 0 {
			if len(files) == 1 {
				info.Action = constant.TaskActionGenerate
			} else if len(files) > 1 {
				info.Action = constant.TaskActionFirstTailGenerate
			}

			// Convert uploaded files to the same raw-base64 representation used by
			// JSON/data-URI image inputs. Keep text image fields so a URL/base64
			// mixture can be rejected instead of being silently replaced.
			images := make([]string, 0, len(files))
			for _, fileHeader := range files {
				// 检查文件大小
				if fileHeader.Size > MaxFileSize {
					return nil, fmt.Errorf("文件 %s 大小超过限制，最大允许 %d MB", fileHeader.Filename, MaxFileSize/(1024*1024))
				}

				file, err := fileHeader.Open()
				if err != nil {
					return nil, fmt.Errorf("open input_reference file %q: %w", fileHeader.Filename, err)
				}
				fileBytes, err := io.ReadAll(file)
				file.Close()
				if err != nil {
					return nil, fmt.Errorf("read input_reference file %q: %w", fileHeader.Filename, err)
				}
				// 将文件内容转换为base64
				base64Str := base64.StdEncoding.EncodeToString(fileBytes)
				images = append(images, base64Str)
			}
			req.Images = append(req.Images, images...)
		}
	}

	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
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

	// Parse Jimeng response
	var jResp responsePayload
	if err := common.Unmarshal(responseBody, &jResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if jResp.Code != 10000 {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s", jResp.Message), fmt.Sprintf("%d", jResp.Code), http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return jResp.Data.TaskID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/?Action=CVSync2AsyncGetResult&Version=2022-08-31", baseUrl)
	if isNewAPIRelay(key) {
		uri = fmt.Sprintf("%s/jimeng/?Action=CVSync2AsyncGetResult&Version=2022-08-31", a.baseURL)
	}
	payload := map[string]string{
		"req_key": "jimeng_vgfm_t2v_l20", // This is fixed value from doc: https://www.volcengine.com/docs/85621/1544774
		"task_id": taskID,
	}
	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal fetch task payload failed")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	if isNewAPIRelay(key) {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		keyParts := strings.Split(key, "|")
		if len(keyParts) != 2 {
			return nil, fmt.Errorf("invalid api key format for jimeng: expected 'ak|sk'")
		}
		accessKey := strings.TrimSpace(keyParts[0])
		secretKey := strings.TrimSpace(keyParts[1])

		if err := a.signRequest(req, accessKey, secretKey); err != nil {
			return nil, errors.Wrap(err, "sign request failed")
		}
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"jimeng_vgfm_t2v_l20"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "jimeng"
}

func (a *TaskAdaptor) signRequest(req *http.Request, accessKey, secretKey string) error {
	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return errors.Wrap(err, "read request body failed")
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Rewind
	} else {
		bodyBytes = []byte{}
	}

	payloadHash := sha256.Sum256(bodyBytes)
	hexPayloadHash := hex.EncodeToString(payloadHash[:])

	t := time.Now().UTC()
	xDate := t.Format("20060102T150405Z")
	shortDate := t.Format("20060102")

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", hexPayloadHash)

	// Sort and encode query parameters to create canonical query string
	queryParams := req.URL.Query()
	sortedKeys := make([]string, 0, len(queryParams))
	for k := range queryParams {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	var queryParts []string
	for _, k := range sortedKeys {
		values := queryParams[k]
		sort.Strings(values)
		for _, v := range values {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
		}
	}
	canonicalQueryString := strings.Join(queryParts, "&")

	headersToSign := map[string]string{
		"host":             req.URL.Host,
		"x-date":           xDate,
		"x-content-sha256": hexPayloadHash,
	}
	if req.Header.Get("Content-Type") != "" {
		headersToSign["content-type"] = req.Header.Get("Content-Type")
	}

	var signedHeaderKeys []string
	for k := range headersToSign {
		signedHeaderKeys = append(signedHeaderKeys, k)
	}
	sort.Strings(signedHeaderKeys)

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headersToSign[k]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		req.URL.Path,
		canonicalQueryString,
		canonicalHeaders.String(),
		signedHeaders,
		hexPayloadHash,
	)

	hashedCanonicalRequest := sha256.Sum256([]byte(canonicalRequest))
	hexHashedCanonicalRequest := hex.EncodeToString(hashedCanonicalRequest[:])

	region := "cn-north-1"
	serviceName := "cv"
	credentialScope := fmt.Sprintf("%s/%s/%s/request", shortDate, region, serviceName)
	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		xDate,
		credentialScope,
		hexHashedCanonicalRequest,
	)

	kDate := hmacSHA256([]byte(secretKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(serviceName))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	authorization := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)
	return nil
}

func hmacSHA256(key []byte, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*requestPayload, error) {
	if err := validateJimengMediaInputs(req); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("video request is required")
	}
	if info == nil {
		return nil, fmt.Errorf("relay info is required")
	}

	r := requestPayload{
		ReqKey: info.UpstreamModelName,
		Prompt: req.Prompt,
	}

	switch req.Duration {
	case 10:
		r.Frames = 241 // 24*10+1 = 241
	default:
		r.Frames = 121 // 24*5+1 = 121
	}

	if err := taskcommon.UnmarshalMetadata(req.Metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	// Public image fields and their compatibility aliases are authoritative over
	// provider metadata. Keep metadata intact for the official Jimeng request
	// route when no public image field was supplied.
	if images := jimengImageInputs(req); len(images) > 0 {
		imageURLs, binaryData, err := splitJimengImageInputs(images)
		if err != nil {
			return nil, err
		}
		r.ImageUrls = imageURLs
		r.BinaryDataBase64 = binaryData
	}
	if err := validateJimengPayloadImageInputs(&r); err != nil {
		return nil, err
	}
	// ValidateBasicTaskRequest canonicalizes ratio/aspectRatio into
	// req.AspectRatio. Metadata is only a compatibility fallback, so a
	// canonical top-level value must not be lost when metadata is absent.
	if req.AspectRatio != "" {
		r.AspectRatio = req.AspectRatio
	}

	// 即梦视频3.0 ReqKey转换
	// https://www.volcengine.com/docs/85621/1792707
	imageLen := len(r.BinaryDataBase64)
	if len(r.ImageUrls) > 0 {
		imageLen = len(r.ImageUrls)
	}
	if strings.Contains(r.ReqKey, "jimeng_v30") {
		if r.ReqKey == "jimeng_v30_pro" {
			// 3.0 pro只有固定的jimeng_ti2v_v30_pro
			r.ReqKey = "jimeng_ti2v_v30_pro"
		} else if imageLen > 1 {
			// 多张图片：首尾帧生成
			r.ReqKey = strings.TrimSuffix(strings.Replace(r.ReqKey, "jimeng_v30", "jimeng_i2v_first_tail_v30", 1), "p")
		} else if imageLen == 1 {
			// 单张图片：图生视频
			r.ReqKey = strings.TrimSuffix(strings.Replace(r.ReqKey, "jimeng_v30", "jimeng_i2v_first_v30", 1), "p")
		} else {
			// 无图片：文生视频
			r.ReqKey = strings.Replace(r.ReqKey, "jimeng_v30", "jimeng_t2v_v30", 1)
		}
	}

	return &r, nil
}

func validateJimengMediaInputs(req *relaycommon.TaskSubmitReq) error {
	if req == nil {
		return fmt.Errorf("video request is required")
	}
	if hasJimengVideoOrAudioInput(req) {
		return fmt.Errorf("Jimeng does not support video or audio reference inputs")
	}
	_, _, err := splitJimengImageInputs(jimengImageInputs(req))
	return err
}

// Multipart files are not represented in TaskSubmitReq. Inspect the one
// Jimeng-specific file field during mapped validation so URL/file conflicts
// and oversized uploads fail before the request reaches billing.
func validateJimengMultipartInputReferences(c *gin.Context, req *relaycommon.TaskSubmitReq) error {
	if c == nil || !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return fmt.Errorf("read Jimeng multipart input_reference: %w", err)
	}
	defer form.RemoveAll()

	files := form.File["input_reference"]
	if len(files) == 0 {
		return nil
	}
	for _, file := range files {
		if file.Size > MaxFileSize {
			return fmt.Errorf("文件 %s 大小超过限制，最大允许 %d MB", file.Filename, MaxFileSize/(1024*1024))
		}
	}
	imageURLs, _, err := splitJimengImageInputs(jimengImageInputs(req))
	if err != nil {
		return err
	}
	if len(imageURLs) > 0 {
		return fmt.Errorf("Jimeng does not support mixing HTTP(S) image URLs with base64 image data in one request")
	}
	return nil
}

func hasJimengVideoOrAudioInput(req *relaycommon.TaskSubmitReq) bool {
	if req == nil {
		return false
	}
	for _, values := range [][]string{req.Videos, req.VideoURLs, req.Audios, req.AudioURLs} {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	for _, item := range req.Content {
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			return true
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			return true
		}
	}
	return false
}

// jimengImageInputs folds all public image spellings into the two Jimeng
// upstream media fields. Values are de-duplicated because ValidateBasicTaskRequest
// may promote image into images for backward compatibility.
func jimengImageInputs(req *relaycommon.TaskSubmitReq) []string {
	if req == nil {
		return nil
	}

	images := make([]string, 0, len(req.Images)+len(req.ImageURLs)+len(req.Content)+len(req.InputStartFrames)+len(req.InputImageReferences)+len(req.MetadataStartFrames)+2)
	for _, values := range [][]string{
		req.Images,
		{req.Image},
		req.ImageURLs,
		{req.InputReference},
		req.InputStartFrames,
		req.InputImageReferences,
		req.MetadataStartFrames,
	} {
		for _, image := range values {
			images = appendJimengImage(images, image)
		}
	}
	for _, item := range req.Content {
		if item.ImageURL != nil {
			images = appendJimengImage(images, item.ImageURL.URL)
		}
	}
	return images
}

func appendJimengImage(images []string, image string) []string {
	image = strings.TrimSpace(image)
	if image == "" {
		return images
	}
	for _, existing := range images {
		if existing == image {
			return images
		}
	}
	return append(images, image)
}

// splitJimengImageInputs enforces the upstream one-of contract: all public
// images must be HTTP(S) URLs or all must be base64 data. A data URI is
// normalized to its raw base64 payload before it is sent upstream.
func splitJimengImageInputs(images []string) ([]string, []string, error) {
	imageURLs := make([]string, 0, len(images))
	binaryData := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if isJimengHTTPURL(image) {
			imageURLs = append(imageURLs, image)
			continue
		}
		base64Data, err := normalizeJimengBase64Image(image)
		if err != nil {
			return nil, nil, err
		}
		binaryData = append(binaryData, base64Data)
	}
	if len(imageURLs) > 0 && len(binaryData) > 0 {
		return nil, nil, fmt.Errorf("Jimeng does not support mixing HTTP(S) image URLs with base64 image data in one request")
	}
	return imageURLs, binaryData, nil
}

func validateJimengPayloadImageInputs(payload *requestPayload) error {
	if payload == nil {
		return fmt.Errorf("Jimeng request payload is required")
	}
	if len(payload.ImageUrls) > 0 && len(payload.BinaryDataBase64) > 0 {
		return fmt.Errorf("Jimeng does not support both image_urls and binary_data_base64 in one request")
	}
	return nil
}

func isJimengHTTPURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func normalizeJimengBase64Image(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		return parseJimengDataURI(value)
	}
	if !isJimengBase64(value) {
		return "", fmt.Errorf("Jimeng image input must be an HTTP(S) URL, image data URI, or raw base64 data")
	}
	return value, nil
}

func parseJimengDataURI(value string) (string, error) {
	comma := strings.Index(value, ",")
	if comma < 0 {
		return "", fmt.Errorf("Jimeng image data URI must contain base64 data")
	}
	metadata := value[len("data:"):comma]
	base64Data := value[comma+1:]
	parts := strings.Split(metadata, ";")
	if len(parts) == 0 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(parts[0])), "image/") {
		return "", fmt.Errorf("Jimeng image data URI must use an image media type")
	}
	hasBase64 := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			hasBase64 = true
			break
		}
	}
	if !hasBase64 || !isJimengBase64(base64Data) {
		return "", fmt.Errorf("Jimeng image data URI must contain valid base64 data")
	}
	return base64Data, nil
}

func isJimengBase64(value string) bool {
	if value == "" {
		return false
	}
	if _, err := base64.StdEncoding.DecodeString(value); err == nil {
		return true
	}
	_, err := base64.RawStdEncoding.DecodeString(value)
	return err == nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	taskResult := relaycommon.TaskInfo{}
	if resTask.Code == 10000 {
		taskResult.Code = 0
	} else {
		taskResult.Code = resTask.Code // todo uni code
		taskResult.Reason = resTask.Message
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
	}
	switch resTask.Data.Status {
	case "in_queue":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "done":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
	}
	taskResult.Url = resTask.Data.VideoUrl
	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var jimengResp responseTask
	if err := common.Unmarshal(originTask.Data, &jimengResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal jimeng task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.SetMetadata("url", jimengResp.Data.VideoUrl)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.CompletionTime()

	if jimengResp.Code != 10000 {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: jimengResp.Message,
			Code:    fmt.Sprintf("%d", jimengResp.Code),
		}
	}

	return common.Marshal(openAIVideo)
}

func isNewAPIRelay(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-")
}
