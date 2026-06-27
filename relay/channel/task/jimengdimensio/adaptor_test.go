package jimengdimensio

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadKeepsMappedUpstreamModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{Model: "Seedance2.0-jimeng", Prompt: "cat"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "Seedance2.0-jimeng",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "jimeng-video-seedance-2.0-vip",
			IsModelMapped:     true,
		},
	}

	payload, err := adaptor.convertToRequestPayload(&req, info)
	require.NoError(t, err)
	require.Equal(t, "jimeng-video-seedance-2.0-vip", payload.Model)
}

func TestConvertToRequestPayloadSelectsFunctionModeByImageCount(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "text only"}, info)
	require.NoError(t, err)
	require.Equal(t, "first_last_frames", payload.FunctionMode)

	payload, err = adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "two images", Images: []string{"https://example.com/1.jpg", "https://example.com/2.jpg"}}, info)
	require.NoError(t, err)
	require.Equal(t, "first_last_frames", payload.FunctionMode)
	require.Equal(t, "https://example.com/1.jpg", payload.ImageFile1)
	require.Equal(t, "https://example.com/2.jpg", payload.ImageFile2)
	require.Empty(t, payload.FilePaths)

	payload, err = adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "many images", Images: []string{"1", "2", "3"}}, info)
	require.NoError(t, err)
	require.Equal(t, "omni_reference", payload.FunctionMode)
}

func TestConvertToRequestPayloadUsesAspectRatioMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng-video-seedance-2.0-vip"}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{
		Prompt:   "cat",
		Metadata: map[string]any{"aspect_ratio": "16:9"},
	}, info)
	require.NoError(t, err)
	require.Equal(t, "16:9", payload.Ratio)
}

func TestConvertToOpenAIVideoUsesSoraCompatibleResponseShape(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1782570791,
		UpdatedAt: 1782571022,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "task_upstream",
		},
		Properties: model.Properties{
			OriginModelName: "Seedance2.0-jimeng",
		},
		Data: []byte(`{
			"task_id":"task_upstream",
			"status":"completed",
			"progress":100,
			"result":{"url":"https://example.com/video.mp4"}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	require.Equal(t, "task_public", got["id"])
	require.Equal(t, "video", got["object"])
	require.Equal(t, "task_upstream", got["task_id"])
	require.Equal(t, "Seedance2.0-jimeng", got["model"])
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://example.com/video.mp4", got["result_url"])
	require.Equal(t, "https://example.com/video.mp4", got["url"])
	require.Equal(t, "https://example.com/video.mp4", got["video_url"])
	require.Equal(t, []any{"https://example.com/video.mp4"}, got["output"])

	video, ok := got["video"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://example.com/video.mp4", video["url"])
}
