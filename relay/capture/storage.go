package capture

import (
	"context"
	"errors"
	"io"
	"sync"
)

var errStorageDisabled = errors.New("relay capture storage is not configured")

type disabledStorage struct{}

func (disabledStorage) Save(context.Context, Artifact) error { return errStorageDisabled }
func (disabledStorage) List(context.Context, ListFilter) (ListResult, error) {
	return ListResult{}, errStorageDisabled
}
func (disabledStorage) Open(context.Context, string, string) (io.ReadCloser, Metadata, error) {
	return nil, Metadata{}, errStorageDisabled
}
func (disabledStorage) DeleteBefore(context.Context, int64) (int, error) {
	return 0, errStorageDisabled
}
func (disabledStorage) Health(context.Context) error { return errStorageDisabled }

var (
	storageMu sync.RWMutex
	storage   Storage = disabledStorage{}
)

func SetStorage(next Storage) {
	storageMu.Lock()
	previous := storage
	if next == nil {
		storage = disabledStorage{}
	} else {
		storage = next
	}
	storageMu.Unlock()
	if closable, ok := previous.(interface{ Close() }); ok {
		closable.Close()
	}
}

func GetStorage() Storage {
	storageMu.RLock()
	defer storageMu.RUnlock()
	return storage
}
