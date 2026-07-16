package hailuo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultWithContextCancelsFileLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	adaptor := &TaskAdaptor{apiKey: "test-key", baseURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := adaptor.ParseTaskResultWithContext(ctx, []byte(`{
		"task_id":"upstream-task",
		"status":"Success",
		"file_id":"file-id",
		"base_resp":{"status_code":0}
	}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.EqualValues(t, model.TaskStatusSuccess, result.Status)
	assert.Empty(t, result.Url)
}
