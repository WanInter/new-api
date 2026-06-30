package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultDoneWithVideoURL(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"grok-image-video",
		"status":"done",
		"progress":100,
		"result_url":"https://example.com/result.mp4",
		"video":{"url":"https://example.com/video.mp4"},
		"output":["https://example.com/output.mp4"]
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/result.mp4", info.Url)
}

func TestExtractResponseTaskVideoURLFallbacks(t *testing.T) {
	require.Equal(t, "https://example.com/video.mp4", extractResponseTaskVideoURL(responseTask{Video: &struct {
		URL string `json:"url,omitempty"`
	}{URL: "https://example.com/video.mp4"}}))
	require.Equal(t, "https://example.com/output.mp4", extractResponseTaskVideoURL(responseTask{Output: []any{"https://example.com/output.mp4"}}))
	require.Equal(t, "https://example.com/object.mp4", extractResponseTaskVideoURL(responseTask{Output: map[string]any{"url": "https://example.com/object.mp4"}}))
}

func TestParseTaskResultAcceptsObjectOutput(t *testing.T) {
	body := []byte(`{
		"id":"task_upstream",
		"model":"grok-image-video",
		"status":"done",
		"progress":100,
		"output":{"url":"https://example.com/object-output.mp4"}
	}`)

	info, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, string(model.TaskStatusSuccess), info.Status)
	require.Equal(t, "https://example.com/object-output.mp4", info.Url)
}

func TestConvertToOpenAIVideoPromotesMetadataURLToSoraResponseShape(t *testing.T) {
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
			OriginModelName: "sd-bak-2",
		},
		Data: []byte(`{
			"id":"task_upstream",
			"object":"video",
			"model":"sd-bak-2",
			"status":"completed",
			"progress":100,
			"metadata":{
				"result_url":"https://example.com/video.mp4",
				"url":"https://example.com/video.mp4",
				"video_url":"https://example.com/video.mp4"
			}
		}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	require.Equal(t, "task_public", got["id"])
	require.Equal(t, "video", got["object"])
	require.Equal(t, "task_upstream", got["task_id"])
	require.Equal(t, "sd-bak-2", got["model"])
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://example.com/video.mp4", got["result_url"])
	require.Equal(t, "https://example.com/video.mp4", got["url"])
	require.Equal(t, "https://example.com/video.mp4", got["video_url"])
	require.Equal(t, []any{"https://example.com/video.mp4"}, got["output"])

	video, ok := got["video"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://example.com/video.mp4", video["url"])
}

func TestNormalizeVideoSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "int", in: 15, want: "15"},
		{name: "float", in: float64(15), want: "15"},
		{name: "string", in: "15", want: "15"},
		{name: "string seconds suffix", in: "15s", want: "15"},
		{name: "string word suffix", in: "15 seconds", want: "15"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeVideoSeconds(tt.in)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeVideoSecondsFromFormUsesDurationFallback(t *testing.T) {
	require.Equal(t, "15", normalizeVideoSecondsFromForm(map[string][]string{"duration": {"15s"}}))
	require.Equal(t, "10", normalizeVideoSecondsFromForm(map[string][]string{"seconds": {"10s"}, "duration": {"15s"}}))
}

func TestApplyVeoReferenceImagesUsesIngredientsForMoreThanTwoImages(t *testing.T) {
	body := map[string]any{
		"images": []any{
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png",
			"https://example.com/4.png",
		},
	}

	applyVeoReferenceImages(body)

	require.NotContains(t, body, "images")
	require.Equal(t, []string{
		"https://example.com/1.png",
		"https://example.com/2.png",
		"https://example.com/3.png",
		"https://example.com/4.png",
	}, body["Ingredients_images"])
}

func TestApplyVeoReferenceImagesUsesImagesForAtMostTwoImages(t *testing.T) {
	body := map[string]any{
		"Ingredients_images": []any{
			"https://example.com/1.png",
			"https://example.com/2.png",
		},
	}

	applyVeoReferenceImages(body)

	require.NotContains(t, body, "Ingredients_images")
	require.Equal(t, []string{
		"https://example.com/1.png",
		"https://example.com/2.png",
	}, body["images"])
}

func TestEstimateVideoSecondsUsesSeedanceGatewayMetadataDuration(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{
		Model:    "seedance-gateway",
		Metadata: map[string]any{"duration": "15"},
	}, &relaycommon.RelayInfo{OriginModelName: "seedance-gateway"})

	require.Equal(t, 15, seconds)
}

func TestEstimateVideoSecondsSeedanceGatewayDefaultsToFifteen(t *testing.T) {
	seconds := estimateVideoSeconds(relaycommon.TaskSubmitReq{Model: "seedance-gateway"}, nil)

	require.Equal(t, 15, seconds)
}

func TestModelListIncludesSeedanceGateway(t *testing.T) {
	require.Contains(t, (&TaskAdaptor{}).GetModelList(), "seedance-gateway")
}
