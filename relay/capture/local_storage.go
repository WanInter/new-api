package capture

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const manifestFilename = "manifest.json"

type LocalStorage struct {
	baseDir     string
	compression PayloadCompression
}

func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	return NewLocalStorageWithCompression(baseDir, PayloadCompressionNone)
}

func NewLocalStorageWithCompression(baseDir string, compression PayloadCompression) (*LocalStorage, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("relay capture local directory is required")
	}
	compression, err := ParsePayloadCompression(string(compression))
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve relay capture directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create relay capture directory: %w", err)
	}
	return &LocalStorage{baseDir: abs, compression: compression}, nil
}

func (s *LocalStorage) Save(_ context.Context, artifact Artifact) error {
	if !sanitizeID(artifact.Metadata.ID) {
		return fmt.Errorf("invalid relay capture id")
	}
	createdAt := time.Unix(artifact.Metadata.CreatedAt, 0).UTC()
	destination := filepath.Join(
		s.baseDir,
		createdAt.Format("2006"),
		createdAt.Format("01"),
		createdAt.Format("02"),
		fmt.Sprintf("channel-%d", artifact.Metadata.ChannelID),
		artifact.Metadata.ID,
	)
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("relay capture already exists: %s", artifact.Metadata.ID)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(destination), ".capture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	if artifact.Metadata.Request.Stored {
		artifact.Metadata.Request.Compression = string(s.compression)
		if err := writeEncryptedPart(filepath.Join(tempDir, PartRequest+".enc"), artifact.RequestBody, s.compression); err != nil {
			return err
		}
	}
	if artifact.Metadata.Response.Stored {
		artifact.Metadata.Response.Compression = string(s.compression)
		if err := writeEncryptedPart(filepath.Join(tempDir, PartResponse+".enc"), artifact.ResponseBody, s.compression); err != nil {
			return err
		}
	}
	manifest, err := common.Marshal(artifact.Metadata)
	if err != nil {
		return err
	}
	if err := writeSecureFile(filepath.Join(tempDir, manifestFilename), manifest); err != nil {
		return err
	}
	return os.Rename(tempDir, destination)
}

func (s *LocalStorage) List(_ context.Context, filter ListFilter) (ListResult, error) {
	items, _, err := s.findMetadata(filter, "")
	if err != nil {
		return ListResult{}, err
	}
	return paginate(items, filter), nil
}

func (s *LocalStorage) Open(_ context.Context, id string, part string) (io.ReadCloser, Metadata, error) {
	if !sanitizeID(id) || !validPart(part) {
		return nil, Metadata{}, fmt.Errorf("invalid relay capture reference")
	}
	_, directories, err := s.findMetadata(ListFilter{}, id)
	if err != nil {
		return nil, Metadata{}, err
	}
	if len(directories) == 0 {
		return nil, Metadata{}, os.ErrNotExist
	}
	directory := directories[0]
	metadata, err := readManifest(filepath.Join(directory, manifestFilename))
	if err != nil {
		return nil, Metadata{}, err
	}
	partMeta := metadata.Request
	if part == PartResponse {
		partMeta = metadata.Response
	}
	if !partMeta.Stored {
		return nil, metadata, os.ErrNotExist
	}
	compression, err := ParsePayloadCompression(partMeta.Compression)
	if err != nil {
		return nil, metadata, err
	}
	body, err := readEncryptedPart(filepath.Join(directory, part+".enc"), compression)
	if err != nil {
		return nil, metadata, err
	}
	return io.NopCloser(strings.NewReader(string(body))), metadata, nil
}

func (s *LocalStorage) DeleteBefore(ctx context.Context, timestamp int64) (int, error) {
	_, directories, err := s.findMetadata(ListFilter{}, "")
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		metadata, err := readManifest(filepath.Join(directory, manifestFilename))
		if err != nil || metadata.CreatedAt >= timestamp {
			continue
		}
		if err := os.RemoveAll(directory); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *LocalStorage) Health(_ context.Context) error {
	info, err := os.Stat(s.baseDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("relay capture local path is not a directory")
	}
	return nil
}

// findMetadata returns matching metadata and their artifact directories.
func (s *LocalStorage) findMetadata(filter ListFilter, id string) ([]Metadata, []string, error) {
	items := make([]Metadata, 0)
	directories := make([]string, 0)
	err := filepath.WalkDir(s.baseDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != manifestFilename {
			return nil
		}
		metadata, err := readManifest(path)
		if err != nil {
			return nil
		}
		if id != "" {
			if metadata.ID == id {
				directories = append(directories, filepath.Dir(path))
				return filepath.SkipDir
			}
			return nil
		}
		if matchesFilter(metadata, filter) {
			items = append(items, metadata)
			directories = append(directories, filepath.Dir(path))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	return items, directories, nil
}

func writeEncryptedPart(path string, body []byte, compression PayloadCompression) error {
	ciphertext, err := encryptPart(body, compression)
	if err != nil {
		return err
	}
	return writeSecureFile(path, []byte(ciphertext))
}

func readEncryptedPart(path string, compression PayloadCompression) ([]byte, error) {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decryptPart(ciphertext, compression)
}

func writeSecureFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func readManifest(path string) (Metadata, error) {
	var metadata Metadata
	body, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	if err := common.Unmarshal(body, &metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}
