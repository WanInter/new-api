package sora

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraCanonicalBillingJSONMatchesOutgoingDuration(t *testing.T) {
	testCases := []struct {
		name          string
		model         string
		baseURL       string
		body          string
		upstreamField string
		want          int
		wantSize      string
	}{
		{name: "sora standard duration precedence", model: sora2Model, body: `{"model":"alias","prompt":"animate","duration":15,"seconds":"4","size":"1280x720"}`, upstreamField: "seconds", want: 15, wantSize: "1280x720"},
		{name: "sora pro seconds suffix", model: sora2ProModel, body: `{"model":"alias","prompt":"animate","seconds":"12 seconds","size":"1792x1024","images":["image.png"]}`, upstreamField: "seconds", want: 12, wantSize: "1792x1024"},
		{name: "tokenstack fixed output", model: "seedance-2-0-15s-slow", baseURL: "https://www.tokenstack.cc", body: `{"model":"alias","prompt":"animate","duration":"10s","aspect_ratio":"9:16","resolution":"720p"}`, upstreamField: "seconds", want: 15, wantSize: "1280x720"},
		{name: "direct duration", model: directSeedance20Model, body: `{"model":"alias","prompt":"animate","duration":"9 sec","size":"720x1280"}`, upstreamField: "seconds", want: 9, wantSize: "720x1280"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-seedance",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelBaseUrl:    testCase.baseURL,
					UpstreamModelName: testCase.model,
				},
			}

			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			canonicalDuration, canonicalSize := canonicalSoraBilling(t, input.Body)
			assert.Equal(t, testCase.want, canonicalDuration)
			assert.Equal(t, testCase.wantSize, canonicalSize)
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			requestBody, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			var upstream map[string]any
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream[testCase.upstreamField])
			require.True(t, ok)
			assert.Equal(t, testCase.want, wireSeconds)
			assert.Equal(t, testCase.wantSize, upstream["size"])
		})
	}
}

func TestSoraCanonicalBillingMultipartMatchesOutgoingDuration(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "alias"))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("duration", "11s"))
	require.NoError(t, writer.WriteField("seconds", "6"))
	require.NoError(t, writer.WriteField("size", "1024x1792"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: sora2ProModel},
	}

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	canonicalDuration, canonicalSize := canonicalSoraBilling(t, input.Body)
	assert.Equal(t, 11, canonicalDuration)
	assert.Equal(t, "1024x1792", canonicalSize)
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

	requestBody, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(encoded), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	require.Equal(t, []string{"11s"}, form.Value["seconds"])
	require.Equal(t, []string{"1024x1792"}, form.Value["size"])
}

func TestSoraCanonicalBillingRejectsUnknownDurationThroughSchema(t *testing.T) {
	for _, body := range []string{
		`{"model":"alias","prompt":"animate"}`,
		`{"model":"alias","prompt":"animate","duration":0}`,
		`{"model":"alias","prompt":"animate","duration":4.5}`,
		`{"model":"alias","prompt":"animate","duration":"invalid"}`,
		`{"model":"alias","prompt":"animate","duration":8,"size":"960x540"}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: directSeedance20Model}}

		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
		common.CleanupBodyStorage(c)
	}
}

func TestFixedSoraCanonicalBillingMatchesOutgoingProfile(t *testing.T) {
	testCases := []struct {
		name      string
		model     string
		baseURL   string
		body      string
		wireField string
		wantSize  string
		wantAudio any
	}{
		{name: "ax fixed", model: axMultimodalVideoModel, body: `{"model":"alias","prompt":"animate","duration":5}`, wireField: "duration", wantAudio: false},
		{name: "sdquan fixed preserves audio", model: sdquanImageVideoModel, body: `{"model":"alias","prompt":"animate","seconds":"12","generate_audio":true}`, wireField: "duration", wantAudio: true},
		{name: "canvas fixed", model: canvasStandardSeedanceModel, body: `{"model":"alias","prompt":"animate","duration":4}`, wireField: "seconds"},
		{name: "tokenstack fixed", model: "seedance-2-0-15s-high", baseURL: "https://www.tokenstack.cc", body: `{"model":"alias","prompt":"animate","duration":4,"size":"720x1280"}`, wireField: "seconds", wantSize: "1280x720"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newSoraJSONTestContext(t, testCase.body)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: testCase.baseURL, UpstreamModelName: testCase.model}}

			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(15), canonical["duration_seconds"])
			if testCase.wantSize == "" {
				assert.NotContains(t, canonical, "size")
			} else {
				assert.Equal(t, testCase.wantSize, canonical["size"])
			}
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			upstream := outgoingSoraJSON(t, c, info)
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream[testCase.wireField])
			require.True(t, ok)
			assert.Equal(t, 15, wireSeconds)
			if testCase.wantSize != "" {
				assert.Equal(t, testCase.wantSize, upstream["size"])
			}
			if testCase.wantAudio != nil {
				assert.Equal(t, testCase.wantAudio, upstream["generate_audio"])
			}
		})
	}
}

func TestSeedanceGatewayCanonicalBillingUsesMetadataContract(t *testing.T) {
	testCases := []struct {
		name         string
		body         string
		wantDuration int
	}{
		{name: "metadata wins over top level", body: `{"model":"seedance-gateway","prompt":"animate","duration":4,"metadata":{"duration":"12","resolution":"720P"}}`, wantDuration: 12},
		{name: "documented duration default", body: `{"model":"seedance-gateway","prompt":"animate","metadata":{"resolution":"720p"}}`, wantDuration: 15},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newSoraJSONTestContext(t, testCase.body)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: seedanceGatewayModel}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))

			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(testCase.wantDuration), canonical["duration_seconds"])
			assert.Equal(t, "720p", canonical["resolution"])
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			upstream := outgoingSoraJSON(t, c, info)
			assert.NotContains(t, upstream, "duration")
			assert.NotContains(t, upstream, "seconds")
			metadata, ok := upstream["metadata"].(map[string]any)
			require.True(t, ok)
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(metadata["duration"])
			require.True(t, ok)
			assert.Equal(t, testCase.wantDuration, wireSeconds)
			assert.Equal(t, "720p", metadata["resolution"])
		})
	}
}

func TestSeedanceGatewayCanonicalBillingRejectsInvalidMetadata(t *testing.T) {
	for _, body := range []string{
		`{"model":"seedance-gateway","prompt":"animate","duration":10,"resolution":"720p"}`,
		`{"model":"seedance-gateway","prompt":"animate","metadata":{"duration":16,"resolution":"720p"}}`,
		`{"model":"seedance-gateway","prompt":"animate","metadata":{"duration":4.5,"resolution":"720p"}}`,
		`{"model":"seedance-gateway","prompt":"animate","metadata":{"duration":15,"resolution":"1080p"}}`,
	} {
		c := newSoraJSONTestContext(t, body)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: seedanceGatewayModel}}
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
	}
}

func TestTokenStackSaleCanonicalBillingMatchesNestedWire(t *testing.T) {
	c := newSoraJSONTestContext(t, `{"model":"alias","prompt":"animate","input":{"prompt":"animate"},"parameters":{"duration":10,"resolution":"1080P"}}`)
	info := tokenStackRelayInfo("https://www.tokenstack.cc", tokenStackMultiModeModel)

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	canonical := canonicalSoraBillingMap(t, input.Body)
	assert.Equal(t, float64(10), canonical["duration_seconds"])
	assert.Equal(t, "1080p", canonical["resolution"])
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

	upstream := outgoingSoraJSON(t, c, info)
	parameters, ok := upstream["parameters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(10), parameters["duration"])
	assert.Equal(t, "1080P", parameters["resolution"])
}

func TestTokenStackSaleCanonicalBillingRejectsInvalidNestedWire(t *testing.T) {
	for _, parameters := range []string{
		`{}`,
		`{"duration":10}`,
		`{"resolution":"720P"}`,
		`{"duration":4,"resolution":"720P"}`,
		`{"duration":12,"resolution":"720P"}`,
		`{"duration":"10","resolution":"720P"}`,
		`{"duration":10.5,"resolution":"720P"}`,
		`{"duration":10,"resolution":"720p"}`,
		`{"duration":10,"resolution":"480P"}`,
	} {
		c := newSoraJSONTestContext(t, `{"model":"alias","prompt":"animate","parameters":`+parameters+`}`)
		info := tokenStackRelayInfo("https://www.tokenstack.cc", tokenStackMultiModeModel)
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
	}
}

func TestTokenStackDoubaoCanonicalBillingMatchesWireAndDefault(t *testing.T) {
	testCases := []struct {
		name         string
		model        string
		body         string
		wantDuration int
	}{
		{name: "explicit", model: tokenStackDoubaoModel, body: `{"model":"alias","prompt":"animate","duration":12}`, wantDuration: 12},
		{name: "default", model: tokenStackDoubaoFastModel, body: `{"model":"alias","prompt":"animate"}`, wantDuration: 5},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newSoraJSONTestContext(t, testCase.body)
			info := tokenStackRelayInfo("https://www.tokenstack.cc", testCase.model)
			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(testCase.wantDuration), canonical["duration_seconds"])
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			upstream := outgoingSoraJSON(t, c, info)
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream["seconds"])
			require.True(t, ok)
			assert.Equal(t, testCase.wantDuration, wireSeconds)
		})
	}
}

func TestTokenStackDoubaoCanonicalBillingRejectsInvalidDuration(t *testing.T) {
	for _, duration := range []string{"0", "3", "16", "4.5", `"invalid"`} {
		c := newSoraJSONTestContext(t, `{"model":"alias","prompt":"animate","duration":`+duration+`}`)
		info := tokenStackRelayInfo("https://www.tokenstack.cc", tokenStackDoubaoModel)
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
	}
}

func TestTokenStackMultiResolutionCanonicalBillingUsesFixedUnit(t *testing.T) {
	for modelName := range tokenStackMultiResolutionModels {
		t.Run(modelName, func(t *testing.T) {
			c := newSoraJSONTestContext(t, `{"model":"alias","prompt":"animate","aspect_ratio":"16:9","seconds":"1"}`)
			info := tokenStackRelayInfo("https://www.tokenstack.cc", modelName)
			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(15), canonical["duration_seconds"])
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			upstream := outgoingSoraJSON(t, c, info)
			assert.Equal(t, "1", upstream["seconds"])
		})
	}
}

func TestTokenStackMultiResolutionCanonicalBillingRejectsWrongUnit(t *testing.T) {
	for _, field := range []string{"", `,"seconds":"15"`, `,"seconds":"2"`, `,"duration":15`} {
		c := newSoraJSONTestContext(t, `{"model":"alias","prompt":"animate","aspect_ratio":"16:9"`+field+`}`)
		info := tokenStackRelayInfo("https://www.tokenstack.cc", tokenStackMultiResolution720FastModel)
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
	}
}

func TestSoraProfileSchemasRejectUnsupportedOrImplicitOutput(t *testing.T) {
	testCases := []struct {
		model string
		body  string
	}{
		{model: sora2Model, body: `{"model":"alias","prompt":"animate","duration":8,"size":"1792x1024"}`},
		{model: veoOmniFlashModel, body: `{"model":"alias","prompt":"animate","duration":8}`},
	}
	for _, testCase := range testCases {
		c := newSoraJSONTestContext(t, testCase.body)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testCase.model}}
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))
	}
}

func TestSpecialSoraCanonicalBillingJSONMatchesOutgoingFields(t *testing.T) {
	testCases := []struct {
		name          string
		model         string
		body          string
		dimension     string
		wantDimension string
		wantDuration  int
	}{
		{
			name:          "otoy mapped size",
			model:         otoySeedanceMiniReferenceModel,
			body:          `{"model":"alias","prompt":"animate","duration":"10s","size":"1280x720"}`,
			dimension:     "resolution",
			wantDimension: "720p",
			wantDuration:  10,
		},
		{
			name:          "grok image video",
			model:         grokImageVideoModel,
			body:          `{"model":"alias","prompt":"animate","duration":20,"aspect_ratio":"16:9","resolution":"720p"}`,
			dimension:     "resolution",
			wantDimension: "720p",
			wantDuration:  20,
		},
		{
			name:          "grok video 15",
			model:         grokVideo15PreviewModel,
			body:          `{"model":"alias","prompt":"animate","duration":15,"resolution":"480p","images":["https://example.com/frame.png"]}`,
			dimension:     "size",
			wantDimension: "480p",
			wantDuration:  15,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{OriginModelName: "public-model", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testCase.model}}
			adaptor := &TaskAdaptor{}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(testCase.wantDuration), canonical["duration_seconds"])
			assert.Equal(t, testCase.wantDimension, canonical[testCase.dimension])
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			requestBody, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			var upstream map[string]any
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream["duration"])
			require.True(t, ok)
			assert.Equal(t, testCase.wantDuration, wireSeconds)
			assert.Equal(t, testCase.wantDimension, upstream[testCase.dimension])
		})
	}
}

func TestSpecialSoraCanonicalBillingMultipartMatchesOutgoingFields(t *testing.T) {
	testCases := []struct {
		name          string
		model         string
		fields        map[string]string
		dimension     string
		wantDimension string
		wantDuration  int
	}{
		{
			name:  "otoy mapped size",
			model: otoySeedanceMiniReferenceModel,
			fields: map[string]string{
				"duration": "11s",
				"size":     "720x1280",
			},
			dimension:     "resolution",
			wantDimension: "720p",
			wantDuration:  11,
		},
		{
			name:  "grok image video",
			model: grokImageVideoModel,
			fields: map[string]string{
				"duration":     "20",
				"aspect_ratio": "16:9",
				"resolution":   "720p",
			},
			dimension:     "resolution",
			wantDimension: "720p",
			wantDuration:  20,
		},
		{
			name:  "grok video 15",
			model: grokVideo15PreviewModel,
			fields: map[string]string{
				"duration":   "15",
				"resolution": "480p",
				"images":     "https://example.com/frame.png",
			},
			dimension:     "size",
			wantDimension: "480p",
			wantDuration:  15,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "alias"))
			require.NoError(t, writer.WriteField("prompt", "animate"))
			for key, value := range testCase.fields {
				require.NoError(t, writer.WriteField(key, value))
			}
			require.NoError(t, writer.Close())

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{OriginModelName: "public-model", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testCase.model}}
			adaptor := &TaskAdaptor{}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			canonical := canonicalSoraBillingMap(t, input.Body)
			assert.Equal(t, float64(testCase.wantDuration), canonical["duration_seconds"])
			assert.Equal(t, testCase.wantDimension, canonical[testCase.dimension])
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

			requestBody, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			require.NoError(t, err)
			form, err := multipart.NewReader(bytes.NewReader(encoded), params["boundary"]).ReadForm(1 << 20)
			require.NoError(t, err)
			t.Cleanup(func() { _ = form.RemoveAll() })
			require.Len(t, form.Value["duration"], 1)
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(form.Value["duration"][0])
			require.True(t, ok)
			assert.Equal(t, testCase.wantDuration, wireSeconds)
			assert.Equal(t, []string{testCase.wantDimension}, form.Value[testCase.dimension])
		})
	}
}

func TestGrokVideo15CanonicalBillingUsesConfirmedDurationDefault(t *testing.T) {
	body := `{"model":"alias","prompt":"animate","resolution":"720p","images":["https://example.com/frame.png"]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{OriginModelName: "public-model", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: grokVideo15PreviewModel}}

	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	canonical := canonicalSoraBillingMap(t, input.Body)
	assert.Equal(t, float64(6), canonical["duration_seconds"])
	assert.Equal(t, "720p", canonical["size"])
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, soraCanonicalFields(t, info)))

	requestBody, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	var upstream map[string]any
	require.NoError(t, common.Unmarshal(encoded, &upstream))
	assert.NotContains(t, upstream, "duration")
	assert.Equal(t, "720p", upstream["size"])
}

func TestSoraCanonicalBillingCapabilityModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	testCases := []struct {
		model        string
		schema       string
		durations    []string
		dimension    string
		dimensionSet []string
	}{
		{model: sora2Model, schema: sora2DurationSizeSchema, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalSizePath, dimensionSet: []string{"720x1280", "1280x720"}},
		{model: sora2ProModel, schema: sora2ProDurationSizeSchema, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalSizePath, dimensionSet: taskcommon.CanonicalOpenAIVideoSizes()},
		{model: seedanceGatewayModel, schema: seedanceGatewayDurationResolutionSchema, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalResolutionPath, dimensionSet: []string{"720p"}},
		{model: axMultimodalVideoModel, schema: fixedDuration15Schema, durations: []string{"15"}},
		{model: sdquanImageVideoModel, schema: fixedDuration15Schema, durations: []string{"15"}},
		{model: canvasStandardSeedanceModel, schema: fixedDuration15Schema, durations: []string{"15"}},
		{model: otoySeedanceMiniReferenceModel, schema: otoyDurationResolutionSchema, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalResolutionPath, dimensionSet: []string{"480p", "720p"}},
		{model: grokImageVideoModel, schema: grokDurationResolutionSchema, durations: integerStrings(1, 20), dimension: taskcommon.CanonicalResolutionPath, dimensionSet: []string{"480p", "720p"}},
		{model: grokVideo15PreviewModel, schema: grok15DurationSizeBillingSchema, durations: integerStrings(1, 20), dimension: taskcommon.CanonicalSizePath, dimensionSet: []string{"480p", "720p"}},
		{model: veoOmniFlashModel, schema: taskcommon.ExplicitRequiredDurationSizeBillingSchema4To15, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalSizePath, dimensionSet: taskcommon.CanonicalOpenAIVideoSizes()},
		{model: veoOmniFlashVideoEditModel, schema: taskcommon.ExplicitRequiredDurationSizeBillingSchema4To15, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalSizePath, dimensionSet: taskcommon.CanonicalOpenAIVideoSizes()},
		{model: directSeedance20Model, schema: taskcommon.ExplicitDurationSizeBillingSchema4To15, durations: integerStrings(4, 15), dimension: taskcommon.CanonicalSizePath, dimensionSet: taskcommon.CanonicalOpenAIVideoSizes()},
		{model: tokenStackMultiModeModel, schema: tokenStackSaleDurationResolutionSchema, durations: []string{"5", "10", "15"}, dimension: taskcommon.CanonicalResolutionPath, dimensionSet: []string{"720p", "1080p"}},
		{model: tokenStackDoubaoModel, schema: tokenStackDoubaoDurationSchema, durations: integerStrings(4, 15)},
		{model: tokenStackDoubaoFastModel, schema: tokenStackDoubaoDurationSchema, durations: integerStrings(4, 15)},
		{model: tokenStackMultiResolution720FastModel, schema: fixedDuration15Schema, durations: []string{"15"}},
		{model: "seedance-2-0-15s-slow", schema: fixedDuration15Size1280x720Schema, durations: []string{"15"}, dimension: taskcommon.CanonicalSizePath, dimensionSet: []string{"1280x720"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.model, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testCase.model}}
			capability := adaptor.GetTaskBillingCapability(info)
			require.NotNil(t, capability)
			assert.Equal(t, testCase.schema, capability.SchemaVersion)
			require.NotEmpty(t, capability.Fields)
			assert.Equal(t, taskcommon.CanonicalDurationSecondsPath, capability.Fields[0].Path)
			assert.Equal(t, testCase.durations, capability.Fields[0].EnumValues)
			if testCase.dimension == "" {
				require.Len(t, capability.Fields, 1)
			} else {
				require.Len(t, capability.Fields, 2)
				assert.Equal(t, testCase.dimension, capability.Fields[1].Path)
				assert.Equal(t, testCase.dimensionSet, capability.Fields[1].EnumValues)
			}
			fields := soraCanonicalFields(t, info)
			require.NoError(t, billingexpr.ValidateCanonicalBillingExpressionMatrix(
				`param("billing.duration_seconds") == 15 ? tier("fifteen", 2) : tier("other", 1)`,
				fields,
			))
		})
	}
	assert.Nil(t, adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "unknown-model"}}))
}

func integerStrings(minimum, maximum int) []string {
	values := make([]string, 0, maximum-minimum+1)
	for value := minimum; value <= maximum; value++ {
		values = append(values, strconv.Itoa(value))
	}
	return values
}

func newSoraJSONTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func outgoingSoraJSON(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo) map[string]any {
	t.Helper()
	requestBody, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	var upstream map[string]any
	require.NoError(t, common.Unmarshal(encoded, &upstream))
	return upstream
}

func canonicalSoraBilling(t *testing.T, body []byte) (int, string) {
	t.Helper()
	var input struct {
		Billing struct {
			DurationSeconds int    `json:"duration_seconds"`
			Size            string `json:"size"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(body, &input))
	return input.Billing.DurationSeconds, input.Billing.Size
}

func canonicalSoraBillingMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var input struct {
		Billing map[string]any `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(body, &input))
	return input.Billing
}

func soraCanonicalFields(t *testing.T, info *relaycommon.RelayInfo) []billingexpr.CanonicalBillingField {
	t.Helper()
	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(fields))
	return fields
}
