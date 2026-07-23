package sora

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrokImageVideoMapsPublicImageRequest(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-image-video",
		"prompt":"animate the reference",
		"seconds":"20",
		"aspect_ratio":"9:16",
		"resolution":"720p",
		"images":["https://example.com/reference.png"]
	}`)
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, grokImageVideoModel, got["model"])
	assert.Equal(t, "ref", got["mode"])
	assert.Equal(t, float64(20), got["duration"])
	assert.Equal(t, "9:16", got["aspect_ratio"])
	assert.Equal(t, "720p", got["resolution"])
	assert.Equal(t, []any{"https://example.com/reference.png"}, got["images_url"])
	assert.NotContains(t, got, "images")
	assert.NotContains(t, got, "seconds")
}

func TestGrokVideo15MapsPublicImageAndResolution(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-video-1.5",
		"prompt":"animate the reference",
		"seconds":"20",
		"aspect_ratio":"9:16",
		"resolution":"720p",
		"images":["https://example.com/first-frame.png"]
	}`)
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, grokVideo15PreviewModel, got["model"])
	assert.Equal(t, float64(20), got["duration"])
	assert.Equal(t, "9:16", got["aspect_ratio"])
	assert.Equal(t, "720p", got["size"])
	assert.Equal(t, []any{"https://example.com/first-frame.png"}, got["images_url"])
	assert.NotContains(t, got, "images")
	assert.NotContains(t, got, "seconds")
	assert.NotContains(t, got, "resolution")
}

func TestGrokImageVideoUsesFrameModeOnlyForExplicitStartFrame(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-image-video",
		"prompt":"animate the start frame",
		"seconds":"10",
		"aspect_ratio":"16:9",
		"resolution":"720p",
		"input":{"start_frames":["https://example.com/first-frame.png"]}
	}`)
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "frame", got["mode"])
	assert.Equal(t, []any{"https://example.com/first-frame.png"}, got["images_url"])
	assert.NotContains(t, got, "input")
}

func TestGrokImageVideoMapsCaseInsensitiveJSONContentType(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-image-video",
		"prompt":"animate the reference",
		"seconds":"10",
		"aspect_ratio":"16:9",
		"resolution":"720p",
		"images":["https://example.com/reference.png"]
	}`)
	c.Request.Header.Set("Content-Type", "Application/JSON")
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "ref", got["mode"])
	assert.Equal(t, []any{"https://example.com/reference.png"}, got["images_url"])
}

func TestGrokVideo15AcceptsImagesURLAlias(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-video-1.5",
		"prompt":"animate the reference",
		"images_url":["https://example.com/first-frame.png"]
	}`)
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, []any{"https://example.com/first-frame.png"}, got["images_url"])
}

func TestGrokVideo15EstimateBillingUsesUpstreamDefaultDuration(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-video-1.5",
		"prompt":"animate the reference",
		"images":["https://example.com/first-frame.png"]
	}`)
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, float64(6), adaptor.EstimateBilling(c, info)["seconds"])
}

func TestGrokVideo15NormalizesMetadataDurationForBillingAndOutput(t *testing.T) {
	raw := `{
		"model":"grok-video-1.5",
		"prompt":"animate the reference",
		"images":["https://example.com/first-frame.png"],
		"metadata":{"duration":"15s","request_id":"keep"}
	}`
	c := newGrokJSONContext(t, raw)
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, float64(15), adaptor.EstimateBilling(c, info)["seconds"])

	billingBody, err := adaptor.NormalizeBillingRequestBody(info, []byte(raw))
	require.NoError(t, err)
	var billing map[string]any
	require.NoError(t, common.Unmarshal(billingBody, &billing))
	assert.Equal(t, float64(15), billing["duration"])
	billingMetadata, ok := billing["metadata"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, billingMetadata, "duration")
	assert.Equal(t, "keep", billingMetadata["request_id"])

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var upstream map[string]any
	require.NoError(t, common.Unmarshal(data, &upstream))
	assert.Equal(t, float64(15), upstream["duration"])
	upstreamMetadata, ok := upstream["metadata"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, upstreamMetadata, "duration")
	assert.Equal(t, "keep", upstreamMetadata["request_id"])
}

func TestGrokMappedValidationRejectsConflictingMetadataDurationBeforeBilling(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want string
	}{
		{
			name: "duration conflicts with metadata",
			wire: `{"model":"grok-video-1.5","prompt":"animate","duration":10,"images":["image.png"],"metadata":{"duration":15}}`,
			want: "duration 15 conflicts with duration 10",
		},
		{
			name: "seconds conflicts with metadata",
			wire: `{"model":"grok-video-1.5","prompt":"animate","seconds":"10","images":["image.png"],"metadata":{"duration":15}}`,
			want: "duration 15 conflicts with seconds 10",
		},
		{
			name: "duration conflicts with seconds even when metadata matches duration",
			wire: `{"model":"grok-video-1.5","prompt":"animate","duration":10,"seconds":"15","images":["image.png"],"metadata":{"duration":10}}`,
			want: "duration 10 conflicts with seconds 15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newGrokJSONContext(t, tt.wire)
			info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
			adaptor := &TaskAdaptor{}

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			taskErr := adaptor.ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Contains(t, taskErr.Message, tt.want)
		})
	}
}

func TestGrokMappedValidationRejectsRemixBeforeBilling(t *testing.T) {
	tests := []struct {
		name       string
		upstream   string
		wantReject bool
	}{
		{name: "grok video 3", upstream: grokImageVideoModel, wantReject: true},
		{name: "grok video 1.5", upstream: grokVideo15PreviewModel, wantReject: true},
		{name: "other sora profile", upstream: sora2Model},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newGrokJSONContext(t, `{"prompt":"remix this"}`)
			info := grokRelayInfo("public-model", tt.upstream)
			info.Action = constant.TaskActionRemix

			taskErr := (&TaskAdaptor{}).ValidateMappedRequest(c, info)
			if !tt.wantReject {
				assert.Nil(t, taskErr)
				return
			}
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "unsupported_action", taskErr.Code)
			assert.True(t, taskErr.LocalError)
			assert.Contains(t, taskErr.Message, "do not support remix")
		})
	}
}

func TestGrokProfileDoesNotApplyAfterMappingToAnotherUpstreamModel(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-video-1.5",
		"prompt":"animate the reference",
		"seconds":"15",
		"resolution":"720p",
		"images":["https://example.com/first-frame.png"]
	}`)
	info := grokRelayInfo("grok-video-1.5", "another-sora-model")
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, adaptor.ValidateMappedRequest(c, info))

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "another-sora-model", got["model"])
	assert.Equal(t, "15", got["seconds"])
	assert.Equal(t, "720p", got["resolution"])
	assert.Equal(t, []any{"https://example.com/first-frame.png"}, got["images"])
	assert.NotContains(t, got, "images_url")
	assert.NotContains(t, got, "duration")
	assert.NotContains(t, got, "size")
}

func TestGrokImageVideoLeavesMissingDurationForUpstreamValidation(t *testing.T) {
	c := newGrokJSONContext(t, `{
		"model":"grok-image-video",
		"prompt":"generate a video",
		"aspect_ratio":"16:9",
		"resolution":"720p"
	}`)
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "text", got["mode"])
	assert.NotContains(t, got, "duration")
	assert.NotContains(t, got, "seconds")
}

func TestGrokMappedValidationRejectsUnsupportedRequestsBeforeBilling(t *testing.T) {
	tests := []struct {
		name  string
		model string
		wire  string
		want  string
	}{
		{
			name:  "image video rejects video and audio references",
			model: grokImageVideoModel,
			wire:  `{"model":"grok-image-video","prompt":"animate","seconds":"10","aspect_ratio":"16:9","resolution":"720p","images":["image.png"],"videos":["reference.mp4"],"audios":["reference.mp3"]}`,
			want:  "does not support video or audio",
		},
		{
			name:  "image video requires supported output",
			model: grokImageVideoModel,
			wire:  `{"model":"grok-image-video","prompt":"animate","seconds":"10","aspect_ratio":"3:4","resolution":"1080p","images":["image.png"]}`,
			want:  "aspect_ratio is required and must be one of",
		},
		{
			name:  "image video rejects multiple explicit start frames",
			model: grokImageVideoModel,
			wire:  `{"model":"grok-image-video","prompt":"animate","seconds":"10","aspect_ratio":"16:9","resolution":"720p","input":{"start_frames":["first.png","second.png"]}}`,
			want:  "mode frame requires exactly 1 image",
		},
		{
			name:  "video 15 rejects pixel size",
			model: grokVideo15PreviewModel,
			wire:  `{"model":"grok-video-1.5","prompt":"animate","size":"960x540","images":["image.png"]}`,
			want:  "supports size 480p or 720p",
		},
		{
			name:  "video 15 rejects conflicting quality fields",
			model: grokVideo15PreviewModel,
			wire:  `{"model":"grok-video-1.5","prompt":"animate","size":"480p","resolution":"720p","images":["image.png"]}`,
			want:  "conflicts with resolution",
		},
		{
			name:  "video 15 rejects fractional duration before DTO truncation",
			model: grokVideo15PreviewModel,
			wire:  `{"model":"grok-video-1.5","prompt":"animate","duration":5.5,"images":["image.png"]}`,
			want:  "duration must be a positive integer",
		},
		{
			name:  "video 15 rejects multiple images",
			model: grokVideo15PreviewModel,
			wire:  `{"model":"grok-video-1.5","prompt":"animate","images":["first.png","second.png"]}`,
			want:  "requires exactly 1 image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newGrokJSONContext(t, tt.wire)
			info := grokRelayInfo("public-grok-model", tt.model)
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			taskErr := adaptor.ValidateMappedRequest(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, "invalid_request", taskErr.Code)
			assert.Contains(t, taskErr.Message, tt.want)
		})
	}
}

func TestGrokVideo15MapsMultipartImageRequest(t *testing.T) {
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.WriteField("model", "grok-video-1.5"))
	require.NoError(t, writer.WriteField("prompt", "animate the reference"))
	require.NoError(t, writer.WriteField("seconds", "15"))
	require.NoError(t, writer.WriteField("aspect_ratio", "9:16"))
	require.NoError(t, writer.WriteField("resolution", "720p"))
	require.NoError(t, writer.WriteField("images", "https://example.com/first-frame.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.Equal(t, []string{grokVideo15PreviewModel}, form.Value["model"])
	assert.Equal(t, []string{"15"}, form.Value["duration"])
	assert.Equal(t, []string{"9:16"}, form.Value["aspect_ratio"])
	assert.Equal(t, []string{"720p"}, form.Value["size"])
	assert.Equal(t, []string{"https://example.com/first-frame.png"}, form.Value["images_url"])
	assert.NotContains(t, form.Value, "images")
	assert.NotContains(t, form.Value, "seconds")
	assert.NotContains(t, form.Value, "resolution")
}

func TestGrokImageVideoMultipartRemovesConsumedStartFrameMetadata(t *testing.T) {
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.WriteField("model", "grok-image-video"))
	require.NoError(t, writer.WriteField("prompt", "animate the start frame"))
	require.NoError(t, writer.WriteField("seconds", "10"))
	require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "720p"))
	require.NoError(t, writer.WriteField("metadata", `{"start_frames":["https://example.com/first-frame.png"],"duration":10,"request_id":"keep"}`))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.Equal(t, []string{"frame"}, form.Value["mode"])
	assert.Equal(t, []string{"https://example.com/first-frame.png"}, form.Value["images_url"])

	var metadata map[string]any
	require.Len(t, form.Value["metadata"], 1)
	require.NoError(t, common.UnmarshalJsonStr(form.Value["metadata"][0], &metadata))
	assert.NotContains(t, metadata, "start_frames")
	assert.NotContains(t, metadata, "duration")
	assert.Equal(t, "keep", metadata["request_id"])
}

func TestGrokImageVideoMapsMultipartMetadataDuration(t *testing.T) {
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.WriteField("model", "grok-image-video"))
	require.NoError(t, writer.WriteField("prompt", "animate the reference"))
	require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "720p"))
	require.NoError(t, writer.WriteField("images", "https://example.com/reference.png"))
	require.NoError(t, writer.WriteField("metadata", `{"duration":"12s","request_id":"keep"}`))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	assert.Equal(t, float64(12), adaptor.EstimateBilling(c, info)["seconds"])

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.Equal(t, []string{"12"}, form.Value["duration"])
	assert.NotContains(t, form.Value, "seconds")

	var metadata map[string]any
	require.Len(t, form.Value["metadata"], 1)
	require.NoError(t, common.UnmarshalJsonStr(form.Value["metadata"][0], &metadata))
	assert.NotContains(t, metadata, "duration")
	assert.Equal(t, "keep", metadata["request_id"])
}

func TestGrokImageVideoMultipartLeavesMissingDurationForUpstreamValidation(t *testing.T) {
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.WriteField("model", "grok-image-video"))
	require.NoError(t, writer.WriteField("prompt", "animate the reference"))
	require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "720p"))
	require.NoError(t, writer.WriteField("images", "https://example.com/reference.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-image-video", grokImageVideoModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Nil(t, adaptor.ValidateMappedRequest(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.NotContains(t, form.Value, "duration")
	assert.NotContains(t, form.Value, "seconds")
}

func TestGrokImageVideoMapsMultipartJSONTextNestedMedia(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		fieldValue string
		imageURL   string
	}{
		{
			name:       "input start frames",
			fieldName:  "input",
			fieldValue: `{"start_frames":["https://example.com/input-first-frame.png"]}`,
			imageURL:   "https://example.com/input-first-frame.png",
		},
		{
			name:       "content first frame role",
			fieldName:  "content",
			fieldValue: `[{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/content-first-frame.png"}}]`,
			imageURL:   "https://example.com/content-first-frame.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input bytes.Buffer
			writer := multipart.NewWriter(&input)
			require.NoError(t, writer.WriteField("model", "grok-image-video"))
			require.NoError(t, writer.WriteField("prompt", "animate the start frame"))
			require.NoError(t, writer.WriteField("seconds", "10"))
			require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
			require.NoError(t, writer.WriteField("resolution", "720p"))
			require.NoError(t, writer.WriteField(tt.fieldName, tt.fieldValue))
			require.NoError(t, writer.Close())

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := grokRelayInfo("grok-image-video", grokImageVideoModel)
			adaptor := &TaskAdaptor{}

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			require.Nil(t, adaptor.ValidateMappedRequest(c, info))
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)

			_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			require.NoError(t, err)
			form, err := multipart.NewReader(bytes.NewReader(data), params["boundary"]).ReadForm(1 << 20)
			require.NoError(t, err)
			defer form.RemoveAll()
			assert.Equal(t, []string{"frame"}, form.Value["mode"])
			assert.Equal(t, []string{tt.imageURL}, form.Value["images_url"])
			assert.NotContains(t, form.Value, tt.fieldName)
		})
	}
}

func TestGrokMappedValidationRejectsBinaryMultipartFiles(t *testing.T) {
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	require.NoError(t, writer.WriteField("model", "grok-video-1.5"))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	file, err := writer.CreateFormFile("images", "first-frame.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("not an URL"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_media_input", taskErr.Code)
	assert.Contains(t, taskErr.Message, "do not support binary multipart files")
}

func TestGrokMappedValidationRejectsUnsupportedFormEncoding(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader("model=grok-video-1.5&prompt=animate&images=https%3A%2F%2Fexample.com%2Ffirst-frame.png"),
	)
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := grokRelayInfo("grok-video-1.5", grokVideo15PreviewModel)
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	taskErr := adaptor.ValidateMappedRequest(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "invalid_request", taskErr.Code)
	assert.Contains(t, taskErr.Message, "require application/json or multipart/form-data")
}

func newGrokJSONContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func grokRelayInfo(originModel, upstreamModel string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: originModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: upstreamModel,
			IsModelMapped:     originModel != upstreamModel,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
}
