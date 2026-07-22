package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	client      *s3.Client
	bucket      string
	prefix      string
	compression PayloadCompression
}

type S3Options struct {
	Bucket          string
	Prefix          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	Compression     PayloadCompression
}

func NewS3Storage(ctx context.Context, options S3Options) (*S3Storage, error) {
	if strings.TrimSpace(options.Bucket) == "" {
		return nil, fmt.Errorf("relay capture S3 bucket is required")
	}
	compression, err := ParsePayloadCompression(string(options.Compression))
	if err != nil {
		return nil, err
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(defaultString(options.Region, "us-east-1")),
	}
	if options.AccessKeyID != "" || options.SecretAccessKey != "" {
		if options.AccessKeyID == "" || options.SecretAccessKey == "" {
			return nil, fmt.Errorf("relay capture S3 access key and secret must be configured together")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(options.AccessKeyID, options.SecretAccessKey, ""),
		))
	}
	config, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load relay capture S3 config: %w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(options.Endpoint), "/")
	client := s3.NewFromConfig(config, func(s3Options *s3.Options) {
		s3Options.UsePathStyle = options.UsePathStyle
		if endpoint != "" {
			s3Options.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &S3Storage{
		client:      client,
		bucket:      strings.TrimSpace(options.Bucket),
		prefix:      strings.Trim(strings.TrimSpace(options.Prefix), "/"),
		compression: compression,
	}, nil
}

func (s *S3Storage) Save(ctx context.Context, artifact Artifact) error {
	if !sanitizeID(artifact.Metadata.ID) {
		return fmt.Errorf("invalid relay capture id")
	}
	prefix := s.artifactPrefix(artifact.Metadata)
	if artifact.Metadata.Request.Stored {
		artifact.Metadata.Request.Compression = string(s.compression)
		if err := s.putEncryptedPart(ctx, path.Join(prefix, PartRequest+".enc"), artifact.RequestBody); err != nil {
			return err
		}
	}
	if artifact.Metadata.Response.Stored {
		artifact.Metadata.Response.Compression = string(s.compression)
		if err := s.putEncryptedPart(ctx, path.Join(prefix, PartResponse+".enc"), artifact.ResponseBody); err != nil {
			return err
		}
	}
	manifest, err := common.Marshal(artifact.Metadata)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(path.Join(prefix, manifestFilename)),
		Body:        bytes.NewReader(manifest),
		ContentType: aws.String("application/json"),
	})
	return err
}

func (s *S3Storage) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	items, _, err := s.findMetadata(ctx, filter, "")
	if err != nil {
		return ListResult{}, err
	}
	return paginate(items, filter), nil
}

func (s *S3Storage) Open(ctx context.Context, id string, part string) (io.ReadCloser, Metadata, error) {
	if !sanitizeID(id) || !validPart(part) {
		return nil, Metadata{}, fmt.Errorf("invalid relay capture reference")
	}
	_, keys, err := s.findMetadata(ctx, ListFilter{}, id)
	if err != nil {
		return nil, Metadata{}, err
	}
	if len(keys) == 0 {
		return nil, Metadata{}, fmt.Errorf("relay capture not found")
	}
	manifestKey := keys[0]
	metadata, err := s.readManifest(ctx, manifestKey)
	if err != nil {
		return nil, Metadata{}, err
	}
	partMeta := metadata.Request
	if part == PartResponse {
		partMeta = metadata.Response
	}
	if !partMeta.Stored {
		return nil, metadata, fmt.Errorf("relay capture part not stored")
	}
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path.Join(path.Dir(manifestKey), part+".enc")),
	})
	if err != nil {
		return nil, metadata, err
	}
	defer object.Body.Close()
	ciphertext, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, metadata, err
	}
	compression, err := ParsePayloadCompression(partMeta.Compression)
	if err != nil {
		return nil, metadata, err
	}
	plaintext, err := decryptPart(ciphertext, compression)
	if err != nil {
		return nil, metadata, err
	}
	return io.NopCloser(strings.NewReader(string(plaintext))), metadata, nil
}

func (s *S3Storage) DeleteBefore(ctx context.Context, timestamp int64) (int, error) {
	items, manifestKeys, err := s.findMetadata(ctx, ListFilter{}, "")
	if err != nil {
		return 0, err
	}
	deleted := 0
	for index, metadata := range items {
		if metadata.CreatedAt >= timestamp {
			continue
		}
		manifestKey := manifestKeys[index]
		keys := []string{manifestKey}
		if metadata.Request.Stored {
			keys = append(keys, path.Join(path.Dir(manifestKey), PartRequest+".enc"))
		}
		if metadata.Response.Stored {
			keys = append(keys, path.Join(path.Dir(manifestKey), PartResponse+".enc"))
		}
		for _, key := range keys {
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}); err != nil {
				return deleted, err
			}
		}
		deleted++
	}
	return deleted, nil
}

func (s *S3Storage) Health(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Storage) artifactPrefix(metadata Metadata) string {
	date := time.Unix(metadata.CreatedAt, 0).UTC()
	parts := []string{date.Format("2006"), date.Format("01"), date.Format("02"), fmt.Sprintf("channel-%d", metadata.ChannelID), metadata.ID}
	if s.prefix != "" {
		parts = append([]string{s.prefix}, parts...)
	}
	return path.Join(parts...)
}

func (s *S3Storage) putEncryptedPart(ctx context.Context, key string, body []byte) error {
	ciphertext, err := encryptPart(body, s.compression)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(ciphertext),
		ContentType: aws.String("application/octet-stream"),
	})
	return err
}

func (s *S3Storage) findMetadata(ctx context.Context, filter ListFilter, id string) ([]Metadata, []string, error) {
	prefix := s.prefix
	if prefix != "" {
		prefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	items := make([]Metadata, 0)
	keys := make([]string, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, object := range page.Contents {
			if object.Key == nil || !strings.HasSuffix(*object.Key, "/"+manifestFilename) {
				continue
			}
			metadata, err := s.readManifest(ctx, *object.Key)
			if err != nil {
				continue
			}
			if id != "" {
				if metadata.ID == id {
					return []Metadata{metadata}, []string{*object.Key}, nil
				}
				continue
			}
			if matchesFilter(metadata, filter) {
				items = append(items, metadata)
				keys = append(keys, *object.Key)
			}
		}
	}
	return items, keys, nil
}

func (s *S3Storage) readManifest(ctx context.Context, key string) (Metadata, error) {
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Metadata{}, err
	}
	defer object.Body.Close()
	body, err := io.ReadAll(object.Body)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := common.Unmarshal(body, &metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
