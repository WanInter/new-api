package capture

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Session struct {
	mu       sync.Mutex
	metadata Metadata
	request  []byte
	response []byte
}

func NewSession(metadata Metadata, requestHeader http.Header, requestContentType string, requestBody []byte, requestSize int64, isStream bool) *Session {
	metadata.ID = uuid.NewString()
	metadata.CreatedAt = common.GetTimestamp()
	metadata.RequestHeaders = sanitizeHeaders(requestHeader)
	metadata.Request.ContentType = requestContentType
	metadata.Outcome = "pending"

	session := &Session{metadata: metadata}
	if isStream {
		session.skip("streaming_not_supported")
		return session
	}
	if !isTextContentType(requestContentType) {
		session.skip("unsupported_request_content_type")
		return session
	}
	if requestSize > MaxTextPayloadBytes() {
		session.skip("request_too_large")
		return session
	}
	session.request = append([]byte(nil), requestBody...)
	session.metadata.Request.Size = int64(len(session.request))
	session.metadata.Request.SHA256 = hash(session.request)
	session.metadata.Request.Stored = true
	return session
}

func (s *Session) AppendResponse(headers http.Header, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metadata.SkippedReason != "" {
		return
	}
	contentType := headers.Get("Content-Type")
	if s.metadata.Response.ContentType == "" {
		s.metadata.Response.ContentType = contentType
		s.metadata.ResponseHeaders = sanitizeHeaders(headers)
	}
	if isStreamContentType(contentType) {
		s.skip("streaming_not_supported")
		return
	}
	if !isTextContentType(contentType) {
		s.skip("unsupported_response_content_type")
		return
	}
	if int64(len(s.response)+len(body)) > MaxTextPayloadBytes() {
		s.skip("response_too_large")
		return
	}
	s.response = append(s.response, body...)
}

// skip discards both payloads so an unsupported response can never leave a
// partially captured request behind.
func (s *Session) skip(reason string) {
	s.metadata.SkippedReason = reason
	s.request = nil
	s.response = nil
	s.metadata.Request.Size = 0
	s.metadata.Request.SHA256 = ""
	s.metadata.Request.Stored = false
	s.metadata.Response.Size = 0
	s.metadata.Response.SHA256 = ""
	s.metadata.Response.Stored = false
}

func (s *Session) Finalize(statusCode int, outcome string, upstreamModel string) Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.StatusCode = statusCode
	s.metadata.Outcome = outcome
	s.metadata.UpstreamModel = upstreamModel
	if s.metadata.SkippedReason == "" {
		s.metadata.Response.Size = int64(len(s.response))
		s.metadata.Response.SHA256 = hash(s.response)
		s.metadata.Response.Stored = true
	}
	return Artifact{
		Metadata:     s.metadata,
		RequestBody:  append([]byte(nil), s.request...),
		ResponseBody: append([]byte(nil), s.response...),
	}
}

func SaveAsync(artifact Artifact) {
	go func() {
		if err := GetStorage().Save(context.Background(), artifact); err != nil {
			common.SysError(fmt.Sprintf("save relay capture failed: %v", err))
		}
	}()
}

func IsConfigured() bool {
	_, disabled := GetStorage().(disabledStorage)
	return !disabled
}

type ResponseWriter struct {
	gin.ResponseWriter
	mu      sync.RWMutex
	session *Session
}

func NewResponseWriter(writer gin.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{ResponseWriter: writer}
}

func (w *ResponseWriter) SetSession(session *Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.session = session
}

func (w *ResponseWriter) Write(body []byte) (int, error) {
	w.capture(body)
	return w.ResponseWriter.Write(body)
}

func (w *ResponseWriter) WriteString(body string) (int, error) {
	w.capture([]byte(body))
	return w.ResponseWriter.WriteString(body)
}

func (w *ResponseWriter) capture(body []byte) {
	w.mu.RLock()
	session := w.session
	w.mu.RUnlock()
	if session != nil {
		session.AppendResponse(w.Header(), body)
	}
}

func isTextContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json") || contentType == "text/plain"
}

func isStreamContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream")
}

func sanitizeHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"accept":            {},
		"anthropic-version": {},
		"content-type":      {},
		"user-agent":        {},
	}
	result := make(map[string]string)
	for name := range headers {
		if _, ok := allowed[strings.ToLower(name)]; !ok {
			continue
		}
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			result[name] = value
		}
	}
	return result
}

func hash(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}
