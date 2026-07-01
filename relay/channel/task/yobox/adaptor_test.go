package yobox

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadSeedance2UsesInputReference(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:   "seedance2",
		Prompt:  "dance",
		Seconds: "12",
		Size:    "720x1280",
		Images:  []string{"https://example.com/ref.png"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"input_reference":"https://example.com/ref.png"`)
	require.Contains(t, string(body), `"seconds":"12"`)
}

func TestConvertToRequestPayloadSeedance20UsesInputObject(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0",
		Prompt:   "run",
		Duration: 6,
		Images:   []string{"https://example.com/start.png", "https://example.com/end.png"},
	}, &relaycommon.RelayInfo{})
	require.NoError(t, err)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"input":`)
	require.Contains(t, string(body), `"start_frames":["https://example.com/start.png"]`)
	require.Contains(t, string(body), `"end_frames":["https://example.com/end.png"]`)
}

func TestConvertToRequestPayloadUsesMappedUpstreamModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0-yo",
		Prompt:   "run",
		Duration: 15,
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		UpstreamModelName: "seedance-2.0",
		IsModelMapped:     true,
	}})
	require.NoError(t, err)

	body, ok := payload.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "seedance-2.0", body["model"])
	require.Contains(t, body, "input")
}

func TestParseTaskResultExtractsOutputsVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{
		"task_id":"task_1",
		"status":"SUCCESS",
		"data":{
			"video_url":"https://example.com/out.mp4",
			"progress":100
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, info.Status)
	require.Equal(t, "https://example.com/out.mp4", info.Url)
}

func TestMergeYoboxRequestMetadataExtractsContentImages(t *testing.T) {
	req := &relaycommon.TaskSubmitReq{
		Metadata: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "prompt"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/1.png"}},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/2.png"}},
			},
		},
	}
	req.Images = mergeYoboxImages(req.Images, extractYoboxContentImages(req.Metadata["content"]))
	require.Equal(t, []string{"https://example.com/1.png", "https://example.com/2.png"}, req.Images)
}

func TestModelListIncludesSupportedModels(t *testing.T) {
	require.Equal(t, []string{"seedance2", "seedance-2.0", "seedance-2.0-fast"}, (&TaskAdaptor{}).GetModelList())
}
