package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/ptr"
)

type taskResultRehostConfig struct {
	Enabled         bool
	Domains         map[string]bool
	Backend         string
	Endpoint        string
	UploadEndpoint  string
	Bucket          string
	Region          string
	PublicBaseURL   string
	Prefix          string
	AccessKeyID     string
	AccessKeySecret string
	Proxy           string
	MaxBytes        int64
	Timeout         time.Duration
}

func TaskResultRehostEnabledForURL(rawURL string) bool {
	cfg := loadTaskResultRehostConfig()
	return cfg.enabledForURL(rawURL)
}

func RehostTaskResultURL(ctx context.Context, task *model.Task, rawURL string, proxy string) (string, error) {
	cfg := loadTaskResultRehostConfig()
	if !cfg.enabledForURL(rawURL) {
		return strings.TrimSpace(rawURL), nil
	}
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if cfg.Backend != "aliyun_oss" && cfg.Backend != "s3" {
		return "", fmt.Errorf("unsupported task result rehost backend: %s", cfg.Backend)
	}
	if err := cfg.validate(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	downloadProxy := cfg.Proxy
	if strings.TrimSpace(downloadProxy) == "" {
		downloadProxy = proxy
	}
	body, contentType, ext, err := downloadRehostSource(ctx, rawURL, downloadProxy, cfg.MaxBytes)
	if err != nil {
		return "", err
	}
	defer body.Close()

	objectKey := cfg.objectKey(task, rawURL, ext)
	if err := cfg.upload(ctx, objectKey, body, contentType); err != nil {
		return "", err
	}
	publicURL := strings.TrimRight(cfg.PublicBaseURL, "/") + "/" + strings.TrimLeft(objectKey, "/")
	logger.LogInfo(ctx, fmt.Sprintf("task result rehosted: task=%s source_host=%s object=%s", task.TaskID, sourceHost(rawURL), objectKey))
	return publicURL, nil
}

func loadTaskResultRehostConfig() taskResultRehostConfig {
	maxMB := common.GetEnvOrDefault("TASK_RESULT_REHOST_MAX_MB", 512)
	if maxMB <= 0 {
		maxMB = 512
	}
	timeoutSeconds := common.GetEnvOrDefault("TASK_RESULT_REHOST_TIMEOUT_SECONDS", 180)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 180
	}
	backend := strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_BACKEND", "aliyun_oss"))
	if backend == "" {
		backend = "aliyun_oss"
	}
	endpoint := strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_ENDPOINT", ""))
	uploadEndpoint := strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_UPLOAD_ENDPOINT", endpoint))
	return taskResultRehostConfig{
		Enabled:         common.GetEnvOrDefaultBool("TASK_RESULT_REHOST_ENABLED", false),
		Domains:         parseRehostDomains(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_DOMAINS", "")),
		Backend:         backend,
		Endpoint:        endpoint,
		UploadEndpoint:  uploadEndpoint,
		Bucket:          strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_BUCKET", "")),
		Region:          strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_REGION", "")),
		PublicBaseURL:   strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_PUBLIC_BASE_URL", "")),
		Prefix:          strings.Trim(strings.TrimSpace(common.GetEnvOrDefaultString("TASK_RESULT_REHOST_PREFIX", "generated/newapi/videos")), "/"),
		AccessKeyID:     strings.TrimSpace(os.Getenv("TASK_RESULT_REHOST_ACCESS_KEY_ID")),
		AccessKeySecret: strings.TrimSpace(os.Getenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET")),
		Proxy:           strings.TrimSpace(os.Getenv("TASK_RESULT_REHOST_PROXY")),
		MaxBytes:        int64(maxMB) * 1024 * 1024,
		Timeout:         time.Duration(timeoutSeconds) * time.Second,
	}
}

func parseRehostDomains(value string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		host := strings.ToLower(strings.TrimSpace(part))
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "https://")
		host = strings.Split(host, "/")[0]
		if host != "" {
			out[host] = true
		}
	}
	return out
}

func (c taskResultRehostConfig) enabledForURL(rawURL string) bool {
	if !c.Enabled || strings.TrimSpace(rawURL) == "" || len(c.Domains) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if c.Domains[host] {
		return true
	}
	for domain := range c.Domains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (c taskResultRehostConfig) validate() error {
	missing := []string{}
	if c.UploadEndpoint == "" {
		missing = append(missing, "TASK_RESULT_REHOST_UPLOAD_ENDPOINT")
	}
	if c.Bucket == "" {
		missing = append(missing, "TASK_RESULT_REHOST_BUCKET")
	}
	if c.Region == "" {
		missing = append(missing, "TASK_RESULT_REHOST_REGION")
	}
	if c.PublicBaseURL == "" {
		missing = append(missing, "TASK_RESULT_REHOST_PUBLIC_BASE_URL")
	}
	if c.AccessKeyID == "" {
		missing = append(missing, "TASK_RESULT_REHOST_ACCESS_KEY_ID")
	}
	if c.AccessKeySecret == "" {
		missing = append(missing, "TASK_RESULT_REHOST_ACCESS_KEY_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("task result rehost config missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func downloadRehostSource(ctx context.Context, rawURL, proxy string, maxBytes int64) (io.ReadCloser, string, string, error) {
	client, err := GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", "", fmt.Errorf("download source failed status=%d body=%s", resp.StatusCode, string(preview))
	}
	if resp.ContentLength > maxBytes {
		defer resp.Body.Close()
		return nil, "", "", fmt.Errorf("download source too large: %d > %d", resp.ContentLength, maxBytes)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}
	ext := extensionFromContentTypeOrURL(contentType, rawURL)
	return &limitedReadCloser{Reader: io.LimitReader(resp.Body, maxBytes+1), Closer: resp.Body, maxBytes: maxBytes}, contentType, ext, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
	maxBytes int64
}

func extensionFromContentTypeOrURL(contentType, rawURL string) string {
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
			return strings.TrimPrefix(exts[0], ".")
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		ext := strings.TrimPrefix(path.Ext(u.Path), ".")
		if ext != "" {
			return ext
		}
	}
	return "mp4"
}

func (c taskResultRehostConfig) objectKey(task *model.Task, rawURL, ext string) string {
	if ext == "" {
		ext = "mp4"
	}
	h := sha256.Sum256([]byte(rawURL))
	datePart := time.Now().Format("20060102")
	name := task.TaskID
	if name == "" {
		name = strconv.FormatInt(task.ID, 10)
	}
	return path.Join(c.Prefix, datePart, fmt.Sprintf("%s-%s.%s", sanitizeObjectName(name), hex.EncodeToString(h[:])[:12], ext))
}

func sanitizeObjectName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (c taskResultRehostConfig) upload(ctx context.Context, objectKey string, body io.Reader, contentType string) error {
	resolver := s3.EndpointResolverFunc(func(region string, options s3.EndpointResolverOptions) (aws.Endpoint, error) {
		return aws.Endpoint{URL: c.UploadEndpoint, SigningRegion: c.Region}, nil
	})
	client := s3.New(s3.Options{
		Region:                     c.Region,
		Credentials:                credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.AccessKeySecret, ""),
		EndpointResolver:           resolver,
		UsePathStyle:               false,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      ptr.String(c.Bucket),
		Key:         ptr.String(objectKey),
		Body:        body,
		ContentType: ptr.String(contentType),
	})
	return err
}

func sourceHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
