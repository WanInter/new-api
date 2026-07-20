package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	taskResultRehostOptionPrefix = "task_result_rehost."

	taskResultRehostOptionConfigured      = taskResultRehostOptionPrefix + "configured"
	taskResultRehostOptionEnabled         = taskResultRehostOptionPrefix + "enabled"
	taskResultRehostOptionDomains         = taskResultRehostOptionPrefix + "domains"
	taskResultRehostOptionBackend         = taskResultRehostOptionPrefix + "backend"
	taskResultRehostOptionUploadEndpoint  = taskResultRehostOptionPrefix + "upload_endpoint"
	taskResultRehostOptionBucket          = taskResultRehostOptionPrefix + "bucket"
	taskResultRehostOptionRegion          = taskResultRehostOptionPrefix + "region"
	taskResultRehostOptionPublicBaseURL   = taskResultRehostOptionPrefix + "public_base_url"
	taskResultRehostOptionUsePathStyle    = taskResultRehostOptionPrefix + "use_path_style"
	taskResultRehostOptionSignedURLExpiry = taskResultRehostOptionPrefix + "signed_url_expiry_hours"
	taskResultRehostOptionPrefixPath      = taskResultRehostOptionPrefix + "prefix"
	taskResultRehostOptionMaxMB           = taskResultRehostOptionPrefix + "max_mb"
	taskResultRehostOptionTimeoutSeconds  = taskResultRehostOptionPrefix + "timeout_seconds"
	taskResultRehostOptionAccessIDSecret  = taskResultRehostOptionPrefix + "access_key_id_secret"
	taskResultRehostOptionAccessKeySecret = taskResultRehostOptionPrefix + "access_key_secret"
	taskResultRehostOptionProxySecret     = taskResultRehostOptionPrefix + "proxy_secret"

	taskResultRehostAccessIDPurpose  = "task-result-rehost/access-key-id"
	taskResultRehostAccessKeyPurpose = "task-result-rehost/access-key-secret"
	taskResultRehostProxyPurpose     = "task-result-rehost/proxy"
)

type TaskResultRehostSettings struct {
	Enabled               bool   `json:"enabled"`
	Domains               string `json:"domains"`
	Backend               string `json:"backend"`
	UploadEndpoint        string `json:"upload_endpoint"`
	Bucket                string `json:"bucket"`
	Region                string `json:"region"`
	PublicBaseURL         string `json:"public_base_url"`
	UsePathStyle          bool   `json:"use_path_style"`
	SignedURLExpiryHours  int    `json:"signed_url_expiry_hours"`
	Prefix                string `json:"prefix"`
	MaxMB                 int    `json:"max_mb"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	CredentialsConfigured bool   `json:"credentials_configured"`
	ProxyConfigured       bool   `json:"proxy_configured"`
	ConfigSource          string `json:"config_source"`
	CredentialSource      string `json:"credential_source"`
	ProxySource           string `json:"proxy_source"`
}

type TaskResultRehostSettingsUpdate struct {
	Enabled              bool    `json:"enabled"`
	Domains              string  `json:"domains"`
	Backend              string  `json:"backend"`
	UploadEndpoint       string  `json:"upload_endpoint"`
	Bucket               string  `json:"bucket"`
	Region               string  `json:"region"`
	PublicBaseURL        string  `json:"public_base_url"`
	UsePathStyle         bool    `json:"use_path_style"`
	SignedURLExpiryHours int     `json:"signed_url_expiry_hours"`
	Prefix               string  `json:"prefix"`
	MaxMB                int     `json:"max_mb"`
	TimeoutSeconds       int     `json:"timeout_seconds"`
	AccessKeyID          *string `json:"access_key_id,omitempty"`
	AccessKeySecret      *string `json:"access_key_secret,omitempty"`
	Proxy                *string `json:"proxy,omitempty"`
	ClearCredentials     bool    `json:"clear_credentials,omitempty"`
	ClearProxy           bool    `json:"clear_proxy,omitempty"`
}

type TaskResultRehostConnectionResult struct {
	ObjectURL string `json:"object_url"`
	LatencyMS int64  `json:"latency_ms"`
	Uploaded  bool   `json:"uploaded"`
	Readable  bool   `json:"readable"`
	CleanedUp bool   `json:"cleaned_up"`
}

type taskResultRehostVerifier func(context.Context, taskResultRehostConfig) (TaskResultRehostConnectionResult, error)

func GetTaskResultRehostSettings() (TaskResultRehostSettings, error) {
	cfg := loadTaskResultRehostConfig()
	if cfg.LoadError != nil {
		return TaskResultRehostSettings{}, cfg.LoadError
	}
	options := taskResultRehostOptionSnapshot()
	return taskResultRehostSettingsView(cfg, options), nil
}

func SaveTaskResultRehostSettings(ctx context.Context, update TaskResultRehostSettingsUpdate) (TaskResultRehostSettings, error) {
	return saveTaskResultRehostSettings(ctx, update, verifyTaskResultRehostStorage)
}

func TestTaskResultRehostSettings(ctx context.Context, update TaskResultRehostSettingsUpdate) (TaskResultRehostConnectionResult, error) {
	cfg, err := taskResultRehostConfigFromUpdate(update)
	if err != nil {
		return TaskResultRehostConnectionResult{}, err
	}
	if err := validateTaskResultRehostSettings(cfg, false); err != nil {
		return TaskResultRehostConnectionResult{}, err
	}
	if err := cfg.validate(); err != nil {
		return TaskResultRehostConnectionResult{}, err
	}
	return verifyTaskResultRehostStorage(ctx, cfg)
}

func saveTaskResultRehostSettings(ctx context.Context, update TaskResultRehostSettingsUpdate, verify taskResultRehostVerifier) (TaskResultRehostSettings, error) {
	cfg, err := taskResultRehostConfigFromUpdate(update)
	if err != nil {
		return TaskResultRehostSettings{}, err
	}
	if err := validateTaskResultRehostSettings(cfg, update.Enabled); err != nil {
		return TaskResultRehostSettings{}, err
	}
	if update.Enabled {
		if _, err = verify(ctx, cfg); err != nil {
			return TaskResultRehostSettings{}, fmt.Errorf("object storage connection test failed: %w", err)
		}
	}

	options, err := taskResultRehostOptionsForStorage(cfg)
	if err != nil {
		return TaskResultRehostSettings{}, err
	}
	if err = model.UpdateOptionsBulk(options); err != nil {
		return TaskResultRehostSettings{}, fmt.Errorf("save task result storage settings: %w", err)
	}
	return GetTaskResultRehostSettings()
}

func taskResultRehostConfigFromUpdate(update TaskResultRehostSettingsUpdate) (taskResultRehostConfig, error) {
	current := loadTaskResultRehostConfig()
	if current.LoadError != nil {
		return taskResultRehostConfig{}, current.LoadError
	}

	cfg := current
	cfg.Enabled = update.Enabled
	cfg.Domains = parseRehostDomains(update.Domains)
	cfg.Backend = strings.ToLower(strings.TrimSpace(update.Backend))
	cfg.Endpoint = strings.TrimSpace(update.UploadEndpoint)
	cfg.UploadEndpoint = strings.TrimSpace(update.UploadEndpoint)
	cfg.Bucket = strings.TrimSpace(update.Bucket)
	cfg.Region = strings.TrimSpace(update.Region)
	cfg.PublicBaseURL = strings.TrimSpace(update.PublicBaseURL)
	cfg.UsePathStyle = update.UsePathStyle
	if update.SignedURLExpiryHours > 0 {
		cfg.SignedURLExpiry = time.Duration(update.SignedURLExpiryHours) * time.Hour
	}
	cfg.Prefix = strings.Trim(strings.TrimSpace(update.Prefix), "/")
	cfg.MaxBytes = int64(update.MaxMB) * 1024 * 1024
	cfg.Timeout = time.Duration(update.TimeoutSeconds) * time.Second

	if update.ClearCredentials {
		cfg.AccessKeyID = ""
		cfg.AccessKeySecret = ""
	} else {
		if update.AccessKeyID != nil {
			cfg.AccessKeyID = strings.TrimSpace(*update.AccessKeyID)
		}
		if update.AccessKeySecret != nil {
			cfg.AccessKeySecret = strings.TrimSpace(*update.AccessKeySecret)
		}
	}
	if update.ClearProxy {
		cfg.Proxy = ""
	} else if update.Proxy != nil {
		cfg.Proxy = strings.TrimSpace(*update.Proxy)
	}

	normalizeTaskResultRehostConfig(&cfg)
	return cfg, nil
}

func validateTaskResultRehostSettings(cfg taskResultRehostConfig, requireComplete bool) error {
	switch cfg.Backend {
	case taskResultRehostBackendAliyunOSS, taskResultRehostBackendIDrive, taskResultRehostBackendS3, taskResultRehostBackendTencentCOS:
	default:
		return fmt.Errorf("unsupported task result rehost backend: %s", cfg.Backend)
	}
	if cfg.MaxBytes <= 0 {
		return fmt.Errorf("max file size must be greater than zero")
	}
	if cfg.MaxBytes > 10*1024*1024*1024 {
		return fmt.Errorf("max file size cannot exceed 10240 MB")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}
	if cfg.Timeout > 30*time.Minute {
		return fmt.Errorf("timeout cannot exceed 1800 seconds")
	}
	if cfg.usesPresignedObjectURLs() && (cfg.SignedURLExpiry <= 0 || cfg.SignedURLExpiry > maxTaskResultRehostSignedURLExpiry) {
		return fmt.Errorf("signed URL expiry must be between 1 and %d hours", int(maxTaskResultRehostSignedURLExpiry/time.Hour))
	}
	if cfg.Prefix == "" || path.Clean(cfg.Prefix) == "." || strings.HasPrefix(path.Clean(cfg.Prefix), "../") {
		return fmt.Errorf("object prefix is invalid")
	}
	urls := map[string]string{"upload endpoint": cfg.UploadEndpoint}
	if !cfg.usesPresignedObjectURLs() {
		urls["public base URL"] = cfg.PublicBaseURL
	}
	for name, rawURL := range urls {
		if rawURL == "" {
			continue
		}
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be a valid HTTP URL", name)
		}
	}
	if requireComplete && len(cfg.Domains) == 0 {
		return fmt.Errorf("at least one rehost source domain is required")
	}
	if requireComplete {
		return cfg.validate()
	}
	return nil
}

func taskResultRehostOptionsForStorage(cfg taskResultRehostConfig) (map[string]string, error) {
	if (cfg.AccessKeyID != "" || cfg.AccessKeySecret != "" || cfg.Proxy != "") && !hasStableSecretEncryptionKey() {
		return nil, fmt.Errorf("CRYPTO_SECRET or SESSION_SECRET must be configured before storing object storage credentials")
	}
	accessID, err := common.EncryptSecret(cfg.AccessKeyID, taskResultRehostAccessIDPurpose)
	if err != nil {
		return nil, err
	}
	accessKey, err := common.EncryptSecret(cfg.AccessKeySecret, taskResultRehostAccessKeyPurpose)
	if err != nil {
		return nil, err
	}
	proxy, err := common.EncryptSecret(cfg.Proxy, taskResultRehostProxyPurpose)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		taskResultRehostOptionConfigured:      "true",
		taskResultRehostOptionEnabled:         strconv.FormatBool(cfg.Enabled),
		taskResultRehostOptionDomains:         formatRehostDomains(cfg.Domains),
		taskResultRehostOptionBackend:         cfg.Backend,
		taskResultRehostOptionUploadEndpoint:  cfg.UploadEndpoint,
		taskResultRehostOptionBucket:          cfg.Bucket,
		taskResultRehostOptionRegion:          cfg.Region,
		taskResultRehostOptionPublicBaseURL:   cfg.PublicBaseURL,
		taskResultRehostOptionUsePathStyle:    strconv.FormatBool(cfg.UsePathStyle),
		taskResultRehostOptionSignedURLExpiry: strconv.FormatInt(int64(cfg.SignedURLExpiry/time.Hour), 10),
		taskResultRehostOptionPrefixPath:      cfg.Prefix,
		taskResultRehostOptionMaxMB:           strconv.FormatInt(cfg.MaxBytes/(1024*1024), 10),
		taskResultRehostOptionTimeoutSeconds:  strconv.FormatInt(int64(cfg.Timeout/time.Second), 10),
		taskResultRehostOptionAccessIDSecret:  accessID,
		taskResultRehostOptionAccessKeySecret: accessKey,
		taskResultRehostOptionProxySecret:     proxy,
	}, nil
}

func applyStoredTaskResultRehostConfig(cfg taskResultRehostConfig) taskResultRehostConfig {
	options := taskResultRehostOptionSnapshot()
	if options[taskResultRehostOptionConfigured] != "true" {
		normalizeTaskResultRehostConfig(&cfg)
		return cfg
	}

	if value, ok := options[taskResultRehostOptionEnabled]; ok {
		cfg.Enabled, _ = strconv.ParseBool(value)
	}
	if value, ok := options[taskResultRehostOptionDomains]; ok {
		cfg.Domains = parseRehostDomains(value)
	}
	if value, ok := options[taskResultRehostOptionBackend]; ok {
		cfg.Backend = strings.ToLower(strings.TrimSpace(value))
	}
	if value, ok := options[taskResultRehostOptionUploadEndpoint]; ok {
		cfg.Endpoint = strings.TrimSpace(value)
		cfg.UploadEndpoint = strings.TrimSpace(value)
	}
	if value, ok := options[taskResultRehostOptionBucket]; ok {
		cfg.Bucket = strings.TrimSpace(value)
	}
	if value, ok := options[taskResultRehostOptionRegion]; ok {
		cfg.Region = strings.TrimSpace(value)
	}
	if value, ok := options[taskResultRehostOptionPublicBaseURL]; ok {
		cfg.PublicBaseURL = strings.TrimSpace(value)
	}
	if value, ok := options[taskResultRehostOptionUsePathStyle]; ok {
		cfg.UsePathStyle, _ = strconv.ParseBool(value)
	}
	if value, ok := options[taskResultRehostOptionSignedURLExpiry]; ok {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.SignedURLExpiry = time.Duration(parsed) * time.Hour
		}
	}
	if value, ok := options[taskResultRehostOptionPrefixPath]; ok {
		cfg.Prefix = strings.Trim(strings.TrimSpace(value), "/")
	}
	if value, ok := options[taskResultRehostOptionMaxMB]; ok {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.MaxBytes = int64(parsed) * 1024 * 1024
		}
	}
	if value, ok := options[taskResultRehostOptionTimeoutSeconds]; ok {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Timeout = time.Duration(parsed) * time.Second
		}
	}

	var err error
	if value, ok := options[taskResultRehostOptionAccessIDSecret]; ok {
		cfg.AccessKeyID, err = common.DecryptSecret(value, taskResultRehostAccessIDPurpose)
		if err != nil {
			cfg.LoadError = fmt.Errorf("decrypt task result storage access key ID: %w", err)
		}
	}
	if value, ok := options[taskResultRehostOptionAccessKeySecret]; ok && cfg.LoadError == nil {
		cfg.AccessKeySecret, err = common.DecryptSecret(value, taskResultRehostAccessKeyPurpose)
		if err != nil {
			cfg.LoadError = fmt.Errorf("decrypt task result storage access key secret: %w", err)
		}
	}
	if value, ok := options[taskResultRehostOptionProxySecret]; ok && cfg.LoadError == nil {
		cfg.Proxy, err = common.DecryptSecret(value, taskResultRehostProxyPurpose)
		if err != nil {
			cfg.LoadError = fmt.Errorf("decrypt task result storage proxy: %w", err)
		}
	}
	normalizeTaskResultRehostConfig(&cfg)
	return cfg
}

func normalizeTaskResultRehostConfig(cfg *taskResultRehostConfig) {
	if cfg.Backend == "" {
		cfg.Backend = taskResultRehostBackendAliyunOSS
	}
	if cfg.Prefix == "" {
		cfg.Prefix = defaultTaskResultRehostPrefix
	}
	if cfg.Backend == taskResultRehostBackendTencentCOS && cfg.Region != "" {
		if cfg.UploadEndpoint == "" {
			cfg.UploadEndpoint = tencentCOSServiceEndpoint(cfg.Region)
		}
		if cfg.Endpoint == "" {
			cfg.Endpoint = cfg.UploadEndpoint
		}
		if cfg.PublicBaseURL == "" && cfg.Bucket != "" {
			cfg.PublicBaseURL = tencentCOSBucketURL(cfg.Bucket, cfg.Region)
		}
	}
	if cfg.Backend == taskResultRehostBackendIDrive {
		cfg.UsePathStyle = true
		cfg.PublicBaseURL = ""
		if cfg.UploadEndpoint == "" && cfg.Region != "" {
			cfg.UploadEndpoint = iDriveE2ServiceEndpoint(cfg.Region)
		}
		if cfg.Endpoint == "" {
			cfg.Endpoint = cfg.UploadEndpoint
		}
	}
}

func taskResultRehostSettingsView(cfg taskResultRehostConfig, options map[string]string) TaskResultRehostSettings {
	configSource := "environment"
	if options[taskResultRehostOptionConfigured] == "true" {
		configSource = "database"
	} else if !hasTaskResultRehostEnvironmentConfig() {
		configSource = "default"
	}
	return TaskResultRehostSettings{
		Enabled:               cfg.Enabled,
		Domains:               formatRehostDomains(cfg.Domains),
		Backend:               cfg.Backend,
		UploadEndpoint:        cfg.UploadEndpoint,
		Bucket:                cfg.Bucket,
		Region:                cfg.Region,
		PublicBaseURL:         cfg.PublicBaseURL,
		UsePathStyle:          cfg.UsePathStyle,
		SignedURLExpiryHours:  int(cfg.SignedURLExpiry / time.Hour),
		Prefix:                cfg.Prefix,
		MaxMB:                 int(cfg.MaxBytes / (1024 * 1024)),
		TimeoutSeconds:        int(cfg.Timeout / time.Second),
		CredentialsConfigured: cfg.AccessKeyID != "" && cfg.AccessKeySecret != "",
		ProxyConfigured:       cfg.Proxy != "",
		ConfigSource:          configSource,
		CredentialSource:      taskResultRehostSecretSource(options, taskResultRehostOptionAccessIDSecret, taskResultRehostOptionAccessKeySecret, "TASK_RESULT_REHOST_ACCESS_KEY_ID", "TASK_RESULT_REHOST_ACCESS_KEY_SECRET"),
		ProxySource:           taskResultRehostSecretSource(options, taskResultRehostOptionProxySecret, "", "TASK_RESULT_REHOST_PROXY", ""),
	}
}

func taskResultRehostSecretSource(options map[string]string, firstOption, secondOption, firstEnv, secondEnv string) string {
	if value, ok := options[firstOption]; ok {
		if value == "" {
			return "none"
		}
		if secondOption == "" || options[secondOption] != "" {
			return "database"
		}
	}
	if os.Getenv(firstEnv) != "" && (secondEnv == "" || os.Getenv(secondEnv) != "") {
		return "environment"
	}
	return "none"
}

func taskResultRehostOptionSnapshot() map[string]string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	options := make(map[string]string)
	for key, value := range common.OptionMap {
		if strings.HasPrefix(key, taskResultRehostOptionPrefix) {
			options[key] = value
		}
	}
	return options
}

func formatRehostDomains(domains map[string]bool) string {
	values := make([]string, 0, len(domains))
	for domain := range domains {
		values = append(values, domain)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func hasStableSecretEncryptionKey() bool {
	return strings.TrimSpace(os.Getenv("CRYPTO_SECRET")) != "" || strings.TrimSpace(os.Getenv("SESSION_SECRET")) != ""
}

func hasTaskResultRehostEnvironmentConfig() bool {
	for _, key := range []string{
		"TASK_RESULT_REHOST_ENABLED",
		"TASK_RESULT_REHOST_DOMAINS",
		"TASK_RESULT_REHOST_BACKEND",
		"TASK_RESULT_REHOST_ENDPOINT",
		"TASK_RESULT_REHOST_UPLOAD_ENDPOINT",
		"TASK_RESULT_REHOST_BUCKET",
		"TASK_RESULT_REHOST_REGION",
		"TASK_RESULT_REHOST_PUBLIC_BASE_URL",
		"TASK_RESULT_REHOST_S3_PATH_STYLE",
		"TASK_RESULT_REHOST_SIGNED_URL_EXPIRY_HOURS",
		"TASK_RESULT_REHOST_PREFIX",
		"TASK_RESULT_REHOST_ACCESS_KEY_ID",
		"TASK_RESULT_REHOST_ACCESS_KEY_SECRET",
		"TASK_RESULT_REHOST_PROXY",
		"TASK_RESULT_REHOST_MAX_MB",
		"TASK_RESULT_REHOST_TIMEOUT_SECONDS",
	} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

func verifyTaskResultRehostStorage(ctx context.Context, cfg taskResultRehostConfig) (TaskResultRehostConnectionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	random, err := common.GenerateRandomCharsKey(16)
	if err != nil {
		return TaskResultRehostConnectionResult{}, fmt.Errorf("generate connection test object key: %w", err)
	}
	objectKey := path.Join(cfg.Prefix, "connection-tests", random+".txt")
	content := []byte("task result storage connection test")
	started := time.Now()
	if err = cfg.upload(ctx, objectKey, bytes.NewReader(content), "text/plain"); err != nil {
		return TaskResultRehostConnectionResult{}, fmt.Errorf("upload test object: %w", err)
	}
	result := TaskResultRehostConnectionResult{Uploaded: true}
	cleanup := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return cfg.deleteObject(cleanupCtx, objectKey)
	}
	defer func() {
		if !result.CleanedUp {
			_ = cleanup()
		}
	}()

	result.ObjectURL, err = cfg.objectURL(ctx, objectKey)
	if err != nil {
		return result, fmt.Errorf("generate test object URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.ObjectURL, nil)
	if err != nil {
		return result, fmt.Errorf("create object read request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("read test object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return result, fmt.Errorf("object read returned status %d: %s", resp.StatusCode, string(preview))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(content)+1)))
	if err != nil {
		return result, fmt.Errorf("read test object response: %w", err)
	}
	if !bytes.Equal(body, content) {
		return result, fmt.Errorf("object read returned unexpected content")
	}
	result.Readable = true
	if err = cleanup(); err != nil {
		return result, fmt.Errorf("delete test object: %w", err)
	}
	result.CleanedUp = true
	result.LatencyMS = time.Since(started).Milliseconds()
	return result, nil
}
