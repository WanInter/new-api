package ali

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingMatchesAliPayload(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "public-wan",
		Prompt:     "cat",
		Duration:   5,
		Resolution: "720p",
		Metadata: map[string]any{
			"parameters": map[string]any{"duration": 9, "audio": true},
		},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-wan",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "wan2.5-i2v-preview", IsModelMapped: true},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	payload, err := adaptor.convertToAliRequest(info, req)
	require.NoError(t, err)
	wire, err := common.Marshal(payload)
	require.NoError(t, err)
	var outbound AliVideoRequest
	require.NoError(t, common.Unmarshal(wire, &outbound))

	input, err := adaptor.BuildBillingInput(c, info)
	require.NoError(t, err)
	var canonical struct {
		Billing struct {
			Duration     int    `json:"duration_seconds"`
			Resolution   string `json:"resolution"`
			AudioEnabled bool   `json:"audio_enabled"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	resolution, err := aliCanonicalResolution(outbound.Parameters)
	require.NoError(t, err)
	assert.Equal(t, outbound.Parameters.Duration, canonical.Billing.Duration)
	assert.Equal(t, resolution, canonical.Billing.Resolution)
	assert.Nil(t, outbound.Parameters.Audio)
	assert.True(t, canonical.Billing.AudioEnabled)

	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestAliRequestAppliesModelAudioPolicy(t *testing.T) {
	trueValue := true
	falseValue := false
	testCases := []struct {
		name        string
		model       string
		audio       *bool
		wantAudio   *bool
		wantErrText string
	}{
		{name: "wan 2.5 matching true is omitted", model: "wan2.5-i2v-preview", audio: &trueValue},
		{name: "wan 2.5 rejects false", model: "wan2.5-i2v-preview", audio: &falseValue, wantErrText: "fixed audio_enabled=true"},
		{name: "wan 2.2 matching false is omitted", model: "wan2.2-i2v-flash", audio: &falseValue},
		{name: "wan 2.2 rejects true", model: "wan2.2-i2v-flash", audio: &trueValue, wantErrText: "fixed audio_enabled=false"},
		{name: "wan 2.6 flash keeps true", model: "wan2.6-i2v-flash", audio: &trueValue, wantAudio: &trueValue},
		{name: "wan 2.6 flash keeps false", model: "wan2.6-i2v-flash", audio: &falseValue, wantAudio: &falseValue},
		{name: "wan 2.6 flash leaves default omitted", model: "wan2.6-i2v-flash"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			metadata := map[string]any{}
			if testCase.audio != nil {
				metadata["parameters"] = map[string]any{"audio": *testCase.audio}
			}
			payload, err := (&TaskAdaptor{}).convertToAliRequest(nil, relaycommon.TaskSubmitReq{
				Model:    testCase.model,
				Prompt:   "cat",
				Metadata: metadata,
			})
			if testCase.wantErrText != "" {
				require.ErrorContains(t, err, testCase.wantErrText)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, payload.Parameters)
			if testCase.wantAudio == nil {
				assert.Nil(t, payload.Parameters.Audio)
				return
			}
			require.NotNil(t, payload.Parameters.Audio)
			assert.Equal(t, *testCase.wantAudio, *payload.Parameters.Audio)
		})
	}
}

func TestAliBillingSchemaEncodesMappedModelResolutionDefault(t *testing.T) {
	adaptor := &TaskAdaptor{}
	capability := adaptor.GetTaskBillingCapability(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "wan2.5-i2v-preview"},
	})
	require.NotNil(t, capability)
	assert.Contains(t, capability.SchemaVersion, "default-1080p")
}

func TestAliCanonicalBillingAppliesModelAudioDefaults(t *testing.T) {
	testCases := []struct {
		name       string
		model      string
		wantAudio  bool
		wantValues []string
	}{
		{name: "wan 2.5 generates audio", model: "wan2.5-i2v-preview", wantAudio: true, wantValues: []string{"true"}},
		{name: "wan 2.2 is silent", model: "wan2.2-i2v-flash", wantAudio: false, wantValues: []string{"false"}},
		{name: "wan 2.6 flash defaults to audio", model: "wan2.6-i2v-flash", wantAudio: true, wantValues: []string{"false", "true"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: testCase.model, Prompt: "cat"})
			info := &relaycommon.RelayInfo{
				OriginModelName: testCase.model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: testCase.model},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
			require.NoError(t, err)
			var canonical struct {
				Billing map[string]any `json:"billing"`
			}
			require.NoError(t, common.Unmarshal(input.Body, &canonical))
			assert.Equal(t, testCase.wantAudio, canonical.Billing["audio_enabled"])

			capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
			require.NotNil(t, capability)
			require.Len(t, capability.Fields, 3)
			assert.True(t, capability.Fields[2].Required)
			assert.Equal(t, testCase.wantValues, capability.Fields[2].EnumValues)
		})
	}
}
