package capture

import (
	"context"
	"io"
	"sort"
	"strings"
)

const (
	PartRequest  = "request"
	PartResponse = "response"
)

type PartMeta struct {
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Stored      bool   `json:"stored"`
}

// Metadata is persisted next to each capture payload. It intentionally
// contains no API keys, authorization headers, or other credential material.
type Metadata struct {
	ID              string            `json:"id"`
	CreatedAt       int64             `json:"created_at"`
	ChannelID       int               `json:"channel_id"`
	ChannelName     string            `json:"channel_name,omitempty"`
	Protocol        string            `json:"protocol"`
	RequestID       string            `json:"request_id,omitempty"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	UserID          int               `json:"user_id,omitempty"`
	TokenID         int               `json:"token_id,omitempty"`
	Model           string            `json:"model,omitempty"`
	UpstreamModel   string            `json:"upstream_model,omitempty"`
	RetryIndex      int               `json:"retry_index,omitempty"`
	Stream          bool              `json:"stream,omitempty"`
	StatusCode      int               `json:"status_code,omitempty"`
	Outcome         string            `json:"outcome"`
	SkippedReason   string            `json:"skipped_reason,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	Request         PartMeta          `json:"request"`
	Response        PartMeta          `json:"response"`
}

type Artifact struct {
	Metadata     Metadata
	RequestBody  []byte
	ResponseBody []byte
}

type ListFilter struct {
	ID        string
	ChannelID int
	Protocol  string
	RequestID string
	Offset    int
	Limit     int
}

type ListResult struct {
	Items []Metadata
	Total int
}

type Storage interface {
	Save(ctx context.Context, artifact Artifact) error
	List(ctx context.Context, filter ListFilter) (ListResult, error)
	Open(ctx context.Context, id string, part string) (io.ReadCloser, Metadata, error)
	DeleteBefore(ctx context.Context, timestamp int64) (int, error)
	Health(ctx context.Context) error
}

func matchesFilter(metadata Metadata, filter ListFilter) bool {
	if filter.ID != "" && metadata.ID != filter.ID {
		return false
	}
	if filter.ChannelID > 0 && metadata.ChannelID != filter.ChannelID {
		return false
	}
	if filter.Protocol != "" && metadata.Protocol != filter.Protocol {
		return false
	}
	return filter.RequestID == "" || metadata.RequestID == filter.RequestID
}

func paginate(items []Metadata, filter ListFilter) ListResult {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	total := len(items)
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return ListResult{Items: []Metadata{}, Total: total}
	}
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return ListResult{Items: items[offset:end], Total: total}
}

func validPart(part string) bool {
	return part == PartRequest || part == PartResponse
}

func sanitizeID(id string) bool {
	return id != "" && !strings.Contains(id, "..") && !strings.ContainsAny(id, "/\\")
}
