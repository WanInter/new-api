package image

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultSuccessUsesFirstImageURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"url":"https://example.com/image.png"}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/image.png", info.Url)
}

func TestParseTaskResultFailureUsesOutputMessage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"FAILED","message":"bad prompt"}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), info.Status)
	require.Equal(t, "bad prompt", info.Reason)
}

func TestParseTaskResultSuccessPreservesDataURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"b64_image":"data:image/jpeg;base64,/9j/abc"}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/jpeg;base64,/9j/abc", info.Url)
}

func TestParseTaskResultSuccessWrapsBareBase64(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"output":{"task_id":"abc","task_status":"SUCCEEDED","results":[{"b64_image":"iVBORw0KGgo="}]}}`)

	info, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "data:image/png;base64,iVBORw0KGgo=", info.Url)
}
