package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTaskResultRehostEnabledForURL(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_ENABLED", "true")
	t.Setenv("TASK_RESULT_REHOST_DOMAINS", "vidgen.x.ai,example.com")
	require.True(t, TaskResultRehostEnabledForURL("https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"))
	require.True(t, TaskResultRehostEnabledForURL("https://sub.example.com/video.mp4"))
	require.False(t, TaskResultRehostEnabledForURL("https://cdn.example.net/video.mp4"))
}

func TestTaskResultRehostEnabledForDataURL(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_ENABLED", "false")
	require.True(t, TaskResultRehostEnabledForDataURL("data:image/png;base64,aW1hZ2U="))
	require.False(t, TaskResultRehostEnabledForDataURL("https://example.com/image.png"))
}

func TestLoadTaskResultRehostConfigTencentCOSDefaults(t *testing.T) {
	t.Setenv("TASK_RESULT_REHOST_BACKEND", "tencent_cos")
	t.Setenv("TASK_RESULT_REHOST_ENDPOINT", "")
	t.Setenv("TASK_RESULT_REHOST_UPLOAD_ENDPOINT", "")
	t.Setenv("TASK_RESULT_REHOST_BUCKET", "media-1250000000")
	t.Setenv("TASK_RESULT_REHOST_REGION", "ap-guangzhou")
	t.Setenv("TASK_RESULT_REHOST_PUBLIC_BASE_URL", "")

	cfg := loadTaskResultRehostConfig()

	require.Equal(t, taskResultRehostBackendTencentCOS, cfg.Backend)
	require.Equal(t, "https://cos.ap-guangzhou.myqcloud.com", cfg.Endpoint)
	require.Equal(t, "https://cos.ap-guangzhou.myqcloud.com", cfg.UploadEndpoint)
	require.Equal(t, "https://media-1250000000.cos.ap-guangzhou.myqcloud.com", cfg.PublicBaseURL)
}

func TestTaskResultRehostConfigValidatesTencentCOS(t *testing.T) {
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendTencentCOS,
		UploadEndpoint:  "https://cos.ap-guangzhou.myqcloud.com",
		Bucket:          "media-1250000000",
		Region:          "ap-guangzhou",
		PublicBaseURL:   "https://media-1250000000.cos.ap-guangzhou.myqcloud.com",
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
	}
	require.NoError(t, cfg.validate())
}

func TestTencentCOSUploadUsesVirtualHostedSignedRequest(t *testing.T) {
	var captured *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendTencentCOS,
		UploadEndpoint:  "https://cos.ap-guangzhou.myqcloud.com",
		Bucket:          "media-1250000000",
		Region:          "ap-guangzhou",
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
	}
	client := cfg.newObjectStorageClient(httpClient)

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      ptr.String(cfg.Bucket),
		Key:         ptr.String("generated/newapi/videos/result.mp4"),
		Body:        bytes.NewReader([]byte("video")),
		ContentType: ptr.String("video/mp4"),
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, "media-1250000000.cos.ap-guangzhou.myqcloud.com", captured.URL.Host)
	require.Equal(t, "/generated/newapi/videos/result.mp4", captured.URL.Path)
	require.True(t, strings.HasPrefix(captured.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=secret-id/"))
}

func TestS3UploadUsesPathStyleSignedRequest(t *testing.T) {
	var captured *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendS3,
		UploadEndpoint:  "https://s3.ap-northeast-1.idrivee2.com",
		Bucket:          "waypeak-work",
		Region:          "ap-northeast-1",
		UsePathStyle:    true,
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
	}
	client := cfg.newObjectStorageClient(httpClient)

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      ptr.String(cfg.Bucket),
		Key:         ptr.String("generated/newapi/videos/result.mp4"),
		Body:        bytes.NewReader([]byte("video")),
		ContentType: ptr.String("video/mp4"),
	})

	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Equal(t, "s3.ap-northeast-1.idrivee2.com", captured.URL.Host)
	require.Equal(t, "/waypeak-work/generated/newapi/videos/result.mp4", captured.URL.Path)
	require.True(t, strings.HasPrefix(captured.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=secret-id/"))
}

func TestTaskResultRehostUploadSetsContentLength(t *testing.T) {
	var captured *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req.Clone(req.Context())
		_, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})}

	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendS3,
		UploadEndpoint:  "https://r2.example.com",
		Bucket:          "media",
		Region:          "auto",
		UsePathStyle:    true,
		AccessKeyID:     "test-access-id",
		AccessKeySecret: "test-access-secret",
	}
	body := &limitedReadCloser{
		Reader:   strings.NewReader("video"),
		Closer:   io.NopCloser(strings.NewReader("")),
		maxBytes: 1024,
	}
	t.Cleanup(func() { require.NoError(t, body.Close()) })

	require.NoError(t, cfg.uploadWithClient(context.Background(), "generated/newapi/videos/result.mp4", body, "video/mp4", int64(len("video")), cfg.newObjectStorageClient(httpClient)))
	require.NotNil(t, captured)
	require.Equal(t, int64(5), captured.ContentLength)
}

func TestIDriveUsesPathStylePresignedObjectURL(t *testing.T) {
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendIDrive,
		Domains:         map[string]bool{"vidgen.x.ai": true},
		UploadEndpoint:  "https://s3.ap-northeast-1.idrivee2.com",
		Bucket:          "waypeak-work",
		Region:          "ap-northeast-1",
		UsePathStyle:    true,
		SignedURLExpiry: defaultTaskResultRehostSignedURLExpiry,
		AccessKeyID:     "test-access-id",
		AccessKeySecret: "test-access-secret",
	}

	objectURL, err := cfg.objectURL(context.Background(), "generated/newapi/videos/result.mp4")

	require.NoError(t, err)
	parsed, err := url.Parse(objectURL)
	require.NoError(t, err)
	require.Equal(t, "s3.ap-northeast-1.idrivee2.com", parsed.Host)
	require.Equal(t, "/waypeak-work/generated/newapi/videos/result.mp4", parsed.Path)
	require.Equal(t, "AWS4-HMAC-SHA256", parsed.Query().Get("X-Amz-Algorithm"))
	require.Equal(t, "604800", parsed.Query().Get("X-Amz-Expires"))
	require.NotEmpty(t, parsed.Query().Get("X-Amz-Signature"))
}

func TestIDriveConfigDoesNotRequirePublicBaseURL(t *testing.T) {
	cfg := taskResultRehostConfig{
		Backend:         taskResultRehostBackendIDrive,
		Domains:         map[string]bool{"vidgen.x.ai": true},
		UploadEndpoint:  "https://s3.ap-northeast-1.idrivee2.com",
		Bucket:          "waypeak-work",
		Region:          "ap-northeast-1",
		UsePathStyle:    true,
		SignedURLExpiry: 24 * time.Hour,
		Prefix:          defaultTaskResultRehostPrefix,
		MaxBytes:        512 * 1024 * 1024,
		Timeout:         180 * time.Second,
		AccessKeyID:     "test-access-id",
		AccessKeySecret: "test-access-secret",
	}

	require.NoError(t, cfg.validate())
	require.NoError(t, validateTaskResultRehostSettings(cfg, true))
}

func TestTaskResultRehostConfigRejectsUnsupportedBackend(t *testing.T) {
	cfg := taskResultRehostConfig{Backend: "unknown"}
	err := cfg.validate()
	require.EqualError(t, err, "unsupported task result rehost backend: unknown")
}

func TestTaskResultRehostSettingsDatabaseOverridesEnvironment(t *testing.T) {
	setupTaskResultRehostSettingsTest(t)
	t.Setenv("TASK_RESULT_REHOST_BACKEND", "aliyun_oss")
	t.Setenv("TASK_RESULT_REHOST_BUCKET", "environment-bucket")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_ID", "environment-id")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET", "environment-secret")

	encryptedID, err := common.EncryptSecret("database-id", taskResultRehostAccessIDPurpose)
	require.NoError(t, err)
	encryptedSecret, err := common.EncryptSecret("database-secret", taskResultRehostAccessKeyPurpose)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		taskResultRehostOptionConfigured:      "true",
		taskResultRehostOptionEnabled:         "true",
		taskResultRehostOptionDomains:         "vidgen.x.ai",
		taskResultRehostOptionBackend:         "tencent_cos",
		taskResultRehostOptionUploadEndpoint:  "https://cos-internal.ap-guangzhou.myqcloud.com",
		taskResultRehostOptionBucket:          "database-bucket-1250000000",
		taskResultRehostOptionRegion:          "ap-guangzhou",
		taskResultRehostOptionPublicBaseURL:   "https://database-bucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		taskResultRehostOptionUsePathStyle:    "true",
		taskResultRehostOptionPrefixPath:      "generated/results",
		taskResultRehostOptionMaxMB:           "256",
		taskResultRehostOptionTimeoutSeconds:  "90",
		taskResultRehostOptionAccessIDSecret:  encryptedID,
		taskResultRehostOptionAccessKeySecret: encryptedSecret,
		taskResultRehostOptionProxySecret:     "",
	}))

	cfg := loadTaskResultRehostConfig()
	require.NoError(t, cfg.validate())
	require.Equal(t, "database-bucket-1250000000", cfg.Bucket)
	require.True(t, cfg.UsePathStyle)
	require.Equal(t, "database-id", cfg.AccessKeyID)
	require.Equal(t, "database-secret", cfg.AccessKeySecret)

	settings, err := GetTaskResultRehostSettings()
	require.NoError(t, err)
	require.Equal(t, "database", settings.ConfigSource)
	require.Equal(t, "database", settings.CredentialSource)
	require.True(t, settings.UsePathStyle)
}

func TestSaveTaskResultRehostSettingsMigratesEnvironmentCredentialsEncrypted(t *testing.T) {
	setupTaskResultRehostSettingsTest(t)
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_ID", "environment-id")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET", "environment-secret")

	settings, err := saveTaskResultRehostSettings(context.Background(), validTaskResultRehostSettingsUpdate(), func(_ context.Context, cfg taskResultRehostConfig) (TaskResultRehostConnectionResult, error) {
		require.Equal(t, "environment-id", cfg.AccessKeyID)
		require.Equal(t, "environment-secret", cfg.AccessKeySecret)
		return TaskResultRehostConnectionResult{Uploaded: true, Readable: true, CleanedUp: true}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "database", settings.ConfigSource)
	require.Equal(t, "database", settings.CredentialSource)

	var storedID model.Option
	require.NoError(t, model.DB.First(&storedID, "key = ?", taskResultRehostOptionAccessIDSecret).Error)
	require.True(t, common.IsEncryptedSecret(storedID.Value))
	require.NotContains(t, storedID.Value, "environment-id")

	var storedSecret model.Option
	require.NoError(t, model.DB.First(&storedSecret, "key = ?", taskResultRehostOptionAccessKeySecret).Error)
	require.True(t, common.IsEncryptedSecret(storedSecret.Value))
	require.NotContains(t, storedSecret.Value, "environment-secret")
}

func TestSaveTaskResultRehostSettingsDoesNotPersistFailedConnection(t *testing.T) {
	setupTaskResultRehostSettingsTest(t)
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_ID", "environment-id")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET", "environment-secret")

	_, err := saveTaskResultRehostSettings(context.Background(), validTaskResultRehostSettingsUpdate(), func(_ context.Context, _ taskResultRehostConfig) (TaskResultRehostConnectionResult, error) {
		return TaskResultRehostConnectionResult{}, fmt.Errorf("storage unavailable")
	})
	require.ErrorContains(t, err, "storage unavailable")

	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key LIKE ?", taskResultRehostOptionPrefix+"%").Count(&count).Error)
	require.Zero(t, count)
}

func TestSaveTaskResultRehostSettingsCanDisableWithoutStorageConnection(t *testing.T) {
	setupTaskResultRehostSettingsTest(t)
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_ID", "environment-id")
	t.Setenv("TASK_RESULT_REHOST_ACCESS_KEY_SECRET", "environment-secret")

	update := validTaskResultRehostSettingsUpdate()
	update.Enabled = false
	settings, err := saveTaskResultRehostSettings(context.Background(), update, func(_ context.Context, _ taskResultRehostConfig) (TaskResultRehostConnectionResult, error) {
		require.FailNow(t, "disabled settings must not require a storage connection")
		return TaskResultRehostConnectionResult{}, nil
	})

	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, "database", settings.ConfigSource)
}

func setupTaskResultRehostSettingsTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.DB.Where("key LIKE ?", taskResultRehostOptionPrefix+"%").Delete(&model.Option{}).Error)

	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	originalCryptoSecret := common.CryptoSecret
	common.CryptoSecret = "task-result-rehost-test-secret"
	t.Setenv("CRYPTO_SECRET", "task-result-rehost-test-secret")

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
	} {
		t.Setenv(key, "")
	}
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
		common.CryptoSecret = originalCryptoSecret
		_ = model.DB.Where("key LIKE ?", taskResultRehostOptionPrefix+"%").Delete(&model.Option{}).Error
	})
}

func validTaskResultRehostSettingsUpdate() TaskResultRehostSettingsUpdate {
	return TaskResultRehostSettingsUpdate{
		Enabled:              true,
		Domains:              "vidgen.x.ai,example.com",
		Backend:              taskResultRehostBackendTencentCOS,
		UploadEndpoint:       "https://cos-internal.ap-guangzhou.myqcloud.com",
		Bucket:               "media-1250000000",
		Region:               "ap-guangzhou",
		PublicBaseURL:        "https://media-1250000000.cos.ap-guangzhou.myqcloud.com",
		SignedURLExpiryHours: 168,
		Prefix:               "generated/newapi/videos",
		MaxMB:                512,
		TimeoutSeconds:       180,
	}
}

func TestDecodeRehostDataURL(t *testing.T) {
	body, contentType, ext, err := decodeRehostDataURL("data:image/png;base64,aW1hZ2U=", 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("image"), body)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, "png", ext)
}

func TestDecodeRehostDataURLRejectsTooLargePayload(t *testing.T) {
	_, _, _, err := decodeRehostDataURL("data:image/png;base64,aW1hZ2U=", 4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
}

func TestTaskResultRehostDataURLUsesImagePrefixByDefault(t *testing.T) {
	cfg := taskResultRehostConfig{Prefix: defaultTaskResultRehostPrefix}
	cfg = cfg.withDataURLPrefix(&model.Task{
		TaskID:   "task_1",
		Platform: constant.TaskPlatformImage,
	})
	key := cfg.objectKey(&model.Task{TaskID: "task_1"}, "data:image/png;base64,aW1hZ2U=", "png")
	require.Contains(t, key, imageTaskResultRehostPrefix+"/")
}

func TestTaskResultRehostDataURLKeepsCustomPrefix(t *testing.T) {
	cfg := taskResultRehostConfig{Prefix: "custom/prefix"}
	cfg = cfg.withDataURLPrefix(&model.Task{
		TaskID:   "task_1",
		Platform: constant.TaskPlatformImage,
	})
	key := cfg.objectKey(&model.Task{TaskID: "task_1"}, "data:image/png;base64,aW1hZ2U=", "png")
	require.Contains(t, key, "custom/prefix/")
}

func TestReplaceRehostedURLInJSON(t *testing.T) {
	oldURL := "https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"
	newURL := "https://cdn.example.com/generated/video.mp4"
	body := []byte(`{"result_url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4","output":["https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"],"video":{"url":"https://vidgen.x.ai/xai-vidgen-bucket/video.mp4"}}`)

	updated := replaceRehostedURLInJSON(body, oldURL, newURL)
	require.NotContains(t, string(updated), oldURL)
	require.Contains(t, string(updated), newURL)
}

func TestReplaceRehostedImageDataURLInJSON(t *testing.T) {
	oldURL := "data:image/png;base64,aW1hZ2U="
	newURL := "https://cdn.example.com/generated/image.png"
	body := []byte(`{"status":"success","result_url":"data:image/png;base64,aW1hZ2U=","data":{"data":[{"b64_json":"aW1hZ2U="}]}}`)

	updated := replaceRehostedImageDataURLInJSON(body, oldURL, newURL)
	require.NotContains(t, string(updated), oldURL)
	require.NotContains(t, string(updated), "b64_json")
	require.Contains(t, string(updated), newURL)
}

func TestImageTaskFailureDataDoesNotExposeBase64(t *testing.T) {
	data := imageTaskFailureData("upload failed")
	require.NotContains(t, string(data), "data:image")
	require.NotContains(t, string(data), "b64_json")
	require.Contains(t, string(data), "upload failed")
}
