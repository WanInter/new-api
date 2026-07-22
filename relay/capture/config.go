package capture

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var maxTextPayloadBytes atomic.Int64

func init() {
	maxTextPayloadBytes.Store(2 << 20)
}

func MaxTextPayloadBytes() int64 {
	return maxTextPayloadBytes.Load()
}

// InitFromEnv configures the global storage backend. An empty storage type
// disables capture even when a channel policy is enabled.
func InitFromEnv(ctx context.Context) error {
	maxTextPayloadBytes.Store(int64(envInt("RELAY_CAPTURE_MAX_TEXT_KB", 2048)) << 10)
	storageType := strings.ToLower(strings.TrimSpace(os.Getenv("RELAY_CAPTURE_STORAGE")))
	switch storageType {
	case "", "disabled", "none":
		SetStorage(nil)
		return nil
	case "local", "s3", "minio":
		if strings.TrimSpace(os.Getenv("CRYPTO_SECRET")) == "" {
			SetStorage(nil)
			return fmt.Errorf("CRYPTO_SECRET is required when relay capture storage is enabled")
		}
	}
	layout := strings.ToLower(strings.TrimSpace(os.Getenv("RELAY_CAPTURE_S3_LAYOUT")))
	if layout == "" {
		layout = "objects"
	}
	compression := PayloadCompressionNone
	if storageType == "local" || ((storageType == "s3" || storageType == "minio") && layout == "objects") {
		var err error
		compression, err = ParsePayloadCompression(os.Getenv("RELAY_CAPTURE_COMPRESSION"))
		if err != nil {
			SetStorage(nil)
			return err
		}
	}
	switch storageType {
	case "local":
		store, err := NewLocalStorageWithCompression(os.Getenv("RELAY_CAPTURE_LOCAL_DIR"), compression)
		if err != nil {
			SetStorage(nil)
			return err
		}
		SetStorage(store)
		return nil
	case "s3", "minio":
		options := S3Options{
			Bucket:          os.Getenv("RELAY_CAPTURE_S3_BUCKET"),
			Prefix:          os.Getenv("RELAY_CAPTURE_S3_PREFIX"),
			Region:          os.Getenv("RELAY_CAPTURE_S3_REGION"),
			Endpoint:        os.Getenv("RELAY_CAPTURE_S3_ENDPOINT"),
			AccessKeyID:     os.Getenv("RELAY_CAPTURE_S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("RELAY_CAPTURE_S3_SECRET_ACCESS_KEY"),
			UsePathStyle:    envBool("RELAY_CAPTURE_S3_USE_PATH_STYLE", storageType == "minio"),
			Compression:     compression,
		}
		if layout == "segments" {
			store, err := NewSegmentStorage(ctx, SegmentOptions{
				S3:            options,
				SpoolDir:      os.Getenv("RELAY_CAPTURE_S3_SPOOL_DIR"),
				MaxBytes:      int64(envInt("RELAY_CAPTURE_S3_SEGMENT_MAX_MB", 64)) << 20,
				FlushInterval: time.Duration(envInt("RELAY_CAPTURE_S3_FLUSH_SECONDS", 900)) * time.Second,
			})
			if err != nil {
				SetStorage(nil)
				return err
			}
			SetStorage(store)
			return nil
		}
		if layout != "objects" {
			SetStorage(nil)
			return fmt.Errorf("unsupported relay capture S3 layout: %s", layout)
		}
		store, err := NewS3Storage(ctx, options)
		if err != nil {
			SetStorage(nil)
			return err
		}
		SetStorage(store)
		return nil
	default:
		SetStorage(nil)
		return fmt.Errorf("unsupported relay capture storage: %s", storageType)
	}
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
