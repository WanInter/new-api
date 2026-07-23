package vertex

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	geminitask "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexVeoSchemaDeclaresAudioSwitch(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-fast-generate-preview"}}
	capability := adaptor.GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	assert.Contains(t, capability.SchemaVersion, "duration-4-6-8.default-8")
	assert.Contains(t, capability.SchemaVersion, "audio-values-false-true.default-false")
	assert.Equal(t, []string{"720p", "1080p", "4k"}, capability.Fields[1].EnumValues)
	assert.Equal(t, []string{"false", "true"}, capability.Fields[2].EnumValues)

	geminiCapability := geminitask.GetGeminiVeoBillingCapability("veo-3.1-fast-generate-preview")
	require.NotNil(t, geminiCapability)
	assert.NotEqual(t, geminiCapability.SchemaVersion, capability.SchemaVersion)
}

func TestVertexCanonicalBillingMatchesExplicitAudioPayload(t *testing.T) {
	testCases := []struct {
		name     string
		metadata map[string]any
		want     bool
	}{
		{name: "default disabled", want: false},
		{name: "explicit enabled", metadata: map[string]any{"generateAudio": true}, want: true},
		{name: "explicit disabled", metadata: map[string]any{"generateAudio": false}, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "cat", Metadata: testCase.metadata})
			info := &relaycommon.RelayInfo{
				OriginModelName: "veo-3.0-generate-001",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.0-generate-001"},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}

			adaptor := &TaskAdaptor{}
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			wire, err := io.ReadAll(body)
			require.NoError(t, err)
			var outbound geminitask.VeoRequestPayload
			require.NoError(t, common.Unmarshal(wire, &outbound))
			require.NotNil(t, outbound.Parameters)
			require.NotNil(t, outbound.Parameters.GenerateAudio)
			assert.Equal(t, testCase.want, *outbound.Parameters.GenerateAudio)

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			var canonical struct {
				Billing map[string]any `json:"billing"`
			}
			require.NoError(t, common.Unmarshal(input.Body, &canonical))
			assert.Equal(t, testCase.want, canonical.Billing["audio_enabled"])
		})
	}
}
