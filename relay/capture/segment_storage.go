package capture

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	segmentArchivePurpose = "relay-capture-segment"
	segmentIndexPurpose   = "relay-capture-segment-index"
)

type SegmentOptions struct {
	S3            S3Options
	SpoolDir      string
	MaxBytes      int64
	FlushInterval time.Duration
}

type SegmentStorage struct {
	objects       *S3Storage
	spool         *LocalStorage
	maxBytes      int64
	flushInterval time.Duration
	mu            sync.RWMutex
	stop          chan struct{}
	stopOnce      sync.Once
}

type segmentIndex struct {
	Version    int        `json:"version"`
	ArchiveKey string     `json:"archive_key"`
	Captures   []Metadata `json:"captures"`
}

type spoolEntry struct {
	metadata  Metadata
	directory string
}

type segmentCapture struct {
	metadata        Metadata
	request         []byte
	response        []byte
	requestPresent  bool
	responsePresent bool
}

func NewSegmentStorage(ctx context.Context, options SegmentOptions) (*SegmentStorage, error) {
	if strings.TrimSpace(options.SpoolDir) == "" {
		return nil, errors.New("relay capture S3 spool directory is required")
	}
	if options.MaxBytes < 1 {
		return nil, errors.New("relay capture S3 segment size must be positive")
	}
	if options.FlushInterval < time.Second {
		return nil, errors.New("relay capture S3 flush interval must be at least one second")
	}
	options.S3.Compression = PayloadCompressionNone
	objects, err := NewS3Storage(ctx, options.S3)
	if err != nil {
		return nil, err
	}
	spool, err := NewLocalStorage(options.SpoolDir)
	if err != nil {
		return nil, err
	}
	storage := &SegmentStorage{
		objects:       objects,
		spool:         spool,
		maxBytes:      options.MaxBytes,
		flushInterval: options.FlushInterval,
		stop:          make(chan struct{}),
	}
	go storage.flushLoop()
	return storage, nil
}

func (s *SegmentStorage) Save(ctx context.Context, artifact Artifact) error {
	if err := s.spool.Save(ctx, artifact); err != nil {
		return err
	}
	return s.flush(ctx, false)
}

func (s *SegmentStorage) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make(map[string]Metadata)
	indexes, err := s.segmentIndexes(ctx)
	if err != nil {
		return ListResult{}, err
	}
	for _, index := range indexes {
		for _, metadata := range index.Captures {
			if matchesFilter(metadata, filter) {
				items[metadata.ID] = metadata
			}
		}
	}
	legacy, _, err := s.objects.findMetadata(ctx, filter, "")
	if err != nil {
		return ListResult{}, err
	}
	for _, metadata := range legacy {
		items[metadata.ID] = metadata
	}
	spooled, _, err := s.spool.findMetadata(filter, "")
	if err != nil {
		return ListResult{}, err
	}
	for _, metadata := range spooled {
		items[metadata.ID] = metadata
	}
	result := make([]Metadata, 0, len(items))
	for _, metadata := range items {
		result = append(result, metadata)
	}
	return paginate(result, filter), nil
}

func (s *SegmentStorage) Open(ctx context.Context, id string, part string) (io.ReadCloser, Metadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !sanitizeID(id) || !validPart(part) {
		return nil, Metadata{}, errors.New("invalid relay capture reference")
	}
	if body, metadata, err := s.spool.Open(ctx, id, part); err == nil {
		return body, metadata, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, metadata, err
	}
	indexes, err := s.segmentIndexes(ctx)
	if err != nil {
		return nil, Metadata{}, err
	}
	for _, index := range indexes {
		for _, metadata := range index.Captures {
			if metadata.ID != id {
				continue
			}
			partMeta := metadata.Request
			if part == PartResponse {
				partMeta = metadata.Response
			}
			if !partMeta.Stored {
				return nil, metadata, os.ErrNotExist
			}
			body, err := s.readArchivePart(ctx, index.ArchiveKey, id, part)
			if err != nil {
				return nil, metadata, err
			}
			return io.NopCloser(bytes.NewReader(body)), metadata, nil
		}
	}
	return s.objects.Open(ctx, id, part)
}

func (s *SegmentStorage) DeleteBefore(ctx context.Context, timestamp int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted, err := s.spool.DeleteBefore(ctx, timestamp)
	if err != nil {
		return deleted, err
	}
	legacyDeleted, err := s.objects.DeleteBefore(ctx, timestamp)
	if err != nil {
		return deleted, err
	}
	deleted += legacyDeleted
	indexes, err := s.segmentIndexes(ctx)
	if err != nil {
		return deleted, err
	}
	for _, index := range indexes {
		remaining := make([]Metadata, 0, len(index.Captures))
		for _, metadata := range index.Captures {
			if metadata.CreatedAt < timestamp {
				deleted++
			} else {
				remaining = append(remaining, metadata)
			}
		}
		if len(remaining) == len(index.Captures) {
			continue
		}
		if len(remaining) > 0 {
			captures, readErr := s.readArchiveCaptures(ctx, index.ArchiveKey, remaining)
			if readErr != nil {
				return deleted, readErr
			}
			if _, writeErr := s.writeReplacementSegment(ctx, captures); writeErr != nil {
				return deleted, writeErr
			}
		}
		if err := s.deleteSegment(ctx, index); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (s *SegmentStorage) Health(ctx context.Context) error {
	if err := s.spool.Health(ctx); err != nil {
		return err
	}
	return s.objects.Health(ctx)
}

func (s *SegmentStorage) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *SegmentStorage) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.flush(context.Background(), false); err != nil {
				common.SysError(fmt.Sprintf("flush relay capture segments failed: %v", err))
			}
		case <-s.stop:
			return
		}
	}
}

func (s *SegmentStorage) flush(ctx context.Context, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.spoolEntries()
	if err != nil {
		return err
	}
	groups := make(map[string][]spoolEntry)
	for _, entry := range entries {
		createdAt := time.Unix(entry.metadata.CreatedAt, 0).UTC()
		key := fmt.Sprintf("%s/channel-%d", createdAt.Format("2006/01/02"), entry.metadata.ChannelID)
		groups[key] = append(groups[key], entry)
	}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].metadata.CreatedAt < group[j].metadata.CreatedAt })
		var total int64
		for _, entry := range group {
			total += entry.metadata.Request.Size + entry.metadata.Response.Size
		}
		if !force && total < s.maxBytes && time.Since(time.Unix(group[0].metadata.CreatedAt, 0)) < s.flushInterval {
			continue
		}
		for _, batch := range splitSegmentEntries(group, s.maxBytes) {
			captures, err := s.loadSpoolCaptures(batch)
			if err != nil {
				return err
			}
			if _, err := s.writeSegment(ctx, captures); err != nil {
				return err
			}
			for _, entry := range batch {
				if err := os.RemoveAll(entry.directory); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *SegmentStorage) spoolEntries() ([]spoolEntry, error) {
	items, directories, err := s.spool.findMetadata(ListFilter{}, "")
	if err != nil {
		return nil, err
	}
	entries := make([]spoolEntry, len(items))
	for i := range items {
		entries[i] = spoolEntry{metadata: items[i], directory: directories[i]}
	}
	return entries, nil
}

func splitSegmentEntries(entries []spoolEntry, maxBytes int64) [][]spoolEntry {
	batches := make([][]spoolEntry, 0)
	batch := make([]spoolEntry, 0)
	var size int64
	for _, entry := range entries {
		entrySize := entry.metadata.Request.Size + entry.metadata.Response.Size
		if len(batch) > 0 && size+entrySize > maxBytes {
			batches = append(batches, batch)
			batch = make([]spoolEntry, 0)
			size = 0
		}
		batch = append(batch, entry)
		size += entrySize
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func (s *SegmentStorage) loadSpoolCaptures(entries []spoolEntry) ([]segmentCapture, error) {
	captures := make([]segmentCapture, 0, len(entries))
	for _, entry := range entries {
		capture := segmentCapture{metadata: entry.metadata}
		if capture.metadata.Request.Stored {
			body, err := readEncryptedPart(filepath.Join(entry.directory, PartRequest+".enc"), PayloadCompressionNone)
			if err != nil {
				return nil, err
			}
			capture.request = body
		}
		if capture.metadata.Response.Stored {
			body, err := readEncryptedPart(filepath.Join(entry.directory, PartResponse+".enc"), PayloadCompressionNone)
			if err != nil {
				return nil, err
			}
			capture.response = body
		}
		captures = append(captures, capture)
	}
	return captures, nil
}

func (s *SegmentStorage) writeSegment(ctx context.Context, captures []segmentCapture) (segmentIndex, error) {
	return s.writeSegmentVersion(ctx, captures, "")
}

// writeReplacementSegment creates a distinct archive before deleting the old
// one during retention. The normal stable key is intentionally retained for
// retries when an index upload fails.
func (s *SegmentStorage) writeReplacementSegment(ctx context.Context, captures []segmentCapture) (segmentIndex, error) {
	return s.writeSegmentVersion(ctx, captures, common.GetUUID())
}

func (s *SegmentStorage) writeSegmentVersion(ctx context.Context, captures []segmentCapture, version string) (segmentIndex, error) {
	if len(captures) == 0 {
		return segmentIndex{}, errors.New("relay capture segment is empty")
	}
	archive, err := encodeSegmentArchive(captures)
	if err != nil {
		return segmentIndex{}, err
	}
	archiveKey, indexKey := s.segmentKeys(captures, version)
	if _, err := s.objects.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.objects.bucket),
		Key:         aws.String(archiveKey),
		Body:        strings.NewReader(archive),
		ContentType: aws.String("application/octet-stream"),
	}); err != nil {
		return segmentIndex{}, err
	}
	index := segmentIndex{Version: 1, ArchiveKey: archiveKey, Captures: make([]Metadata, len(captures))}
	for i, capture := range captures {
		index.Captures[i] = capture.metadata
	}
	encodedIndex, err := common.Marshal(index)
	if err != nil {
		return segmentIndex{}, err
	}
	encryptedIndex, err := common.EncryptSecret(string(encodedIndex), segmentIndexPurpose)
	if err != nil {
		return segmentIndex{}, err
	}
	_, err = s.objects.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.objects.bucket),
		Key:         aws.String(indexKey),
		Body:        strings.NewReader(encryptedIndex),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		if _, deleteErr := s.objects.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.objects.bucket),
			Key:    aws.String(archiveKey),
		}); deleteErr != nil {
			return segmentIndex{}, fmt.Errorf("write relay capture segment index: %w (delete unindexed archive: %v)", err, deleteErr)
		}
		return segmentIndex{}, err
	}
	return index, err
}

func encodeSegmentArchive(captures []segmentCapture) (string, error) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, capture := range captures {
		manifest, err := common.Marshal(capture.metadata)
		if err != nil {
			return "", err
		}
		if err := writeTarFile(tarWriter, path.Join(capture.metadata.ID, manifestFilename), manifest); err != nil {
			return "", err
		}
		if capture.metadata.Request.Stored {
			if err := writeTarFile(tarWriter, path.Join(capture.metadata.ID, PartRequest), capture.request); err != nil {
				return "", err
			}
		}
		if capture.metadata.Response.Stored {
			if err := writeTarFile(tarWriter, path.Join(capture.metadata.ID, PartResponse), capture.response); err != nil {
				return "", err
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}
	return common.EncryptSecret(compressed.String(), segmentArchivePurpose)
}

func writeTarFile(writer *tar.Writer, name string, body []byte) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
		return err
	}
	_, err := writer.Write(body)
	return err
}

func (s *SegmentStorage) segmentKeys(captures []segmentCapture, version string) (string, string) {
	first := captures[0].metadata
	last := captures[len(captures)-1].metadata
	createdAt := time.Unix(first.CreatedAt, 0).UTC()
	base := path.Join(s.objects.prefix, createdAt.Format("2006"), createdAt.Format("01"), createdAt.Format("02"), fmt.Sprintf("channel-%d", first.ChannelID), "segments")
	name := fmt.Sprintf("segment-%d-%s-%s", first.CreatedAt, first.ID, last.ID)
	if version != "" {
		name += "-" + version
	}
	return path.Join(base, name+".tar.gz.enc"), path.Join(base, name+".index.enc")
}

func (s *SegmentStorage) segmentIndexes(ctx context.Context) ([]segmentIndex, error) {
	prefix := s.objects.prefix
	if prefix != "" {
		prefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.objects.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.objects.bucket), Prefix: aws.String(prefix)})
	indexes := make([]segmentIndex, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			if object.Key == nil || !strings.HasSuffix(*object.Key, ".index.enc") {
				continue
			}
			index, err := s.readSegmentIndex(ctx, *object.Key)
			if err != nil {
				return nil, err
			}
			indexes = append(indexes, index)
		}
	}
	return indexes, nil
}

func (s *SegmentStorage) readSegmentIndex(ctx context.Context, key string) (segmentIndex, error) {
	object, err := s.objects.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.objects.bucket), Key: aws.String(key)})
	if err != nil {
		return segmentIndex{}, err
	}
	defer object.Body.Close()
	ciphertext, err := io.ReadAll(object.Body)
	if err != nil {
		return segmentIndex{}, err
	}
	plaintext, err := common.DecryptSecret(string(ciphertext), segmentIndexPurpose)
	if err != nil {
		return segmentIndex{}, err
	}
	var index segmentIndex
	if err := common.Unmarshal([]byte(plaintext), &index); err != nil {
		return segmentIndex{}, err
	}
	if index.Version != 1 || index.ArchiveKey == "" {
		return segmentIndex{}, errors.New("invalid relay capture segment index")
	}
	return index, nil
}

func (s *SegmentStorage) readArchivePart(ctx context.Context, archiveKey string, id string, part string) ([]byte, error) {
	captures, err := s.readArchiveCaptures(ctx, archiveKey, nil)
	if err != nil {
		return nil, err
	}
	for _, capture := range captures {
		if capture.metadata.ID == id {
			if part == PartRequest {
				return capture.request, nil
			}
			return capture.response, nil
		}
	}
	return nil, os.ErrNotExist
}

func (s *SegmentStorage) readArchiveCaptures(ctx context.Context, archiveKey string, wanted []Metadata) ([]segmentCapture, error) {
	object, err := s.objects.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.objects.bucket), Key: aws.String(archiveKey)})
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	ciphertext, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, err
	}
	plaintext, err := common.DecryptSecret(string(ciphertext), segmentArchivePurpose)
	if err != nil {
		return nil, err
	}
	wantedIDs := make(map[string]struct{}, len(wanted))
	for _, metadata := range wanted {
		wantedIDs[metadata.ID] = struct{}{}
	}
	return decodeSegmentArchive([]byte(plaintext), wantedIDs)
}

func decodeSegmentArchive(compressed []byte, wantedIDs map[string]struct{}) ([]segmentCapture, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	captures := make(map[string]*segmentCapture)
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		parts := strings.Split(header.Name, "/")
		if len(parts) != 2 || !sanitizeID(parts[0]) || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) {
			return nil, errors.New("invalid relay capture segment entry")
		}
		if len(wantedIDs) > 0 {
			if _, ok := wantedIDs[parts[0]]; !ok {
				continue
			}
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, err
		}
		capture := captures[parts[0]]
		if capture == nil {
			capture = &segmentCapture{}
			captures[parts[0]] = capture
		}
		switch parts[1] {
		case manifestFilename:
			if capture.metadata.ID != "" {
				return nil, errors.New("duplicate relay capture segment manifest")
			}
			if err := common.Unmarshal(body, &capture.metadata); err != nil {
				return nil, err
			}
			if capture.metadata.ID != parts[0] {
				return nil, errors.New("relay capture segment manifest id mismatch")
			}
		case PartRequest:
			if capture.requestPresent {
				return nil, errors.New("duplicate relay capture segment request")
			}
			capture.request = body
			capture.requestPresent = true
		case PartResponse:
			if capture.responsePresent {
				return nil, errors.New("duplicate relay capture segment response")
			}
			capture.response = body
			capture.responsePresent = true
		default:
			return nil, errors.New("invalid relay capture segment part")
		}
	}
	result := make([]segmentCapture, 0, len(captures))
	for _, capture := range captures {
		if capture.metadata.ID == "" {
			return nil, errors.New("relay capture segment is missing metadata")
		}
		if capture.metadata.Request.Stored != capture.requestPresent || capture.metadata.Response.Stored != capture.responsePresent {
			return nil, errors.New("relay capture segment payload metadata mismatch")
		}
		result = append(result, *capture)
	}
	if len(wantedIDs) > 0 && len(result) != len(wantedIDs) {
		return nil, errors.New("relay capture segment is missing requested capture")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].metadata.CreatedAt < result[j].metadata.CreatedAt })
	return result, nil
}

func (s *SegmentStorage) deleteSegment(ctx context.Context, index segmentIndex) error {
	archiveKey := index.ArchiveKey
	indexKey := strings.TrimSuffix(archiveKey, ".tar.gz.enc") + ".index.enc"
	for _, key := range []string{archiveKey, indexKey} {
		if _, err := s.objects.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.objects.bucket), Key: aws.String(key)}); err != nil {
			return err
		}
	}
	return nil
}
