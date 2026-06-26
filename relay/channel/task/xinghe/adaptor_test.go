package xinghe

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertToRequestPayloadNormalizesXingheParams(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_local"}}
	req := &relaycommon.TaskSubmitReq{
		Prompt:   "city",
		Images:   []string{"https://example.com/a.png", "https://example.com/b.png"},
		Duration: 20,
		Metadata: map[string]any{"aspect_ratio": "9:16", "resolution": "1080p"},
	}

	payload, err := adaptor.convertToRequestPayload(req, info)
	require.NoError(t, err)
	require.Equal(t, "xinghe-2.0", payload.Model)
	require.Equal(t, 15, payload.Duration)
	require.Equal(t, "9:16", payload.Ratio)
	require.Equal(t, "1080p", payload.Resolution)
	require.Equal(t, []string{"https://example.com/a.png", "https://example.com/b.png"}, payload.ImageURLs)
	require.Equal(t, "task_local", payload.ClientTaskID)
}

func TestConvertToRequestPayloadRequiresMaterial(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-mini"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	_, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "text only"}, info)
	require.ErrorContains(t, err, "requires at least one image")
}

func TestConvertToRequestPayloadAcceptsVideoAndAudioAliases(t *testing.T) {
	adaptor := &TaskAdaptor{}
	metadata := map[string]any{
		"video_urls":           []any{"https://example.com/v1.mp4"},
		"audio_urls":           []any{"https://example.com/a1.mp3"},
		"reference_video_urls": []any{"https://example.com/ref.mp4"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-fast"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	payload, err := adaptor.convertToRequestPayload(&relaycommon.TaskSubmitReq{Prompt: "city", Metadata: metadata}, info)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/v1.mp4"}, payload.VideoURLs)
	require.Equal(t, []string{"https://example.com/a1.mp3"}, payload.AudioURLs)
	require.Equal(t, []string{"https://example.com/ref.mp4"}, payload.ReferenceVideoURLs)
}

func TestConvertToRequestPayloadKeepsMappedModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "xinghe-2.0", IsModelMapped: true}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	req := &relaycommon.TaskSubmitReq{Model: "public-xinghe", Prompt: "city", Images: []string{"https://example.com/a.png"}}

	payload, err := adaptor.convertToRequestPayload(req, info)
	require.NoError(t, err)
	require.Equal(t, "xinghe-2.0", payload.Model)
}

func TestParseTaskResultExtractsNestedURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{"task_status":"completed","data":{"metadata":{"result_urls":["https://example.com/v.mp4"]}}}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", info.Status)
	require.Equal(t, "https://example.com/v.mp4", info.Url)
}
