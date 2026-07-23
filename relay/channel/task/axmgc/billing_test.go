package axmgc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingUsesAxmgcFixedEffectiveSpecification(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "omitted duration",
			body: `{"model":"seedance-2-720p-933","content":[{"type":"text","text":"cat"}]}`,
		},
		{
			name: "explicit duration does not change fixed model output",
			body: `{"model":"seedance-2-720p-933","content":[{"type":"text","text":"cat"}],"duration":6,"generate_audio":true}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(testCase.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: Seedance720p933Model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: Seedance720p933Model},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
			}
			adaptor := &TaskAdaptor{}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			var canonical struct {
				Billing map[string]any `json:"billing"`
			}
			require.NoError(t, common.Unmarshal(input.Body, &canonical))
			assert.Equal(t, float64(axmgcEffectiveDuration), canonical.Billing["duration_seconds"])
			assert.Equal(t, axmgcEffectiveResolution, canonical.Billing["resolution"])
			assert.NotContains(t, canonical.Billing, "audio_enabled")

			capability := adaptor.GetTaskBillingCapability(info)
			require.NotNil(t, capability)
			require.Len(t, capability.Fields, 2)
			assert.Equal(t, []string{"15"}, capability.Fields[0].EnumValues)
			assert.Equal(t, []string{"720p"}, capability.Fields[1].EnumValues)
			fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
			for _, field := range capability.Fields {
				fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
			}
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
		})
	}
}

func TestAxmgcBillingUsesFinalMappedModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	mappedInfo := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: Seedance720p933Model,
			IsModelMapped:     true,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	require.NotNil(t, adaptor.GetTaskBillingCapability(mappedInfo))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(
		`{"model":"public-seedance","content":[{"type":"text","text":"cat"}],"duration":4}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, mappedInfo))
	input, err := adaptor.BuildBillingInput(c, mappedInfo)
	require.NoError(t, err)
	var canonical struct {
		Billing map[string]any `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(input.Body, &canonical))
	assert.Equal(t, float64(axmgcEffectiveDuration), canonical.Billing["duration_seconds"])
	assert.Equal(t, axmgcEffectiveResolution, canonical.Billing["resolution"])

	unknownInfo := &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "unknown-axmgc-model",
			IsModelMapped:     true,
		},
	}
	assert.Nil(t, adaptor.GetTaskBillingCapability(unknownInfo))
	unknownInput, err := adaptor.BuildBillingInput(c, unknownInfo)
	require.NoError(t, err)
	assert.Empty(t, unknownInput.Body)
}
