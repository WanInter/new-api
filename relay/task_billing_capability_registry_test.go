package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestNonYoboxVideoTaskAdaptorsDeclareCanonicalBilling(t *testing.T) {
	testCases := []struct {
		name              string
		channelType       int
		extraMappedModels []string
	}{
		{name: "Ali", channelType: constant.ChannelTypeAli},
		{name: "Kling", channelType: constant.ChannelTypeKling},
		{name: "Jimeng", channelType: constant.ChannelTypeJimeng},
		{name: "Jimeng Dimensio", channelType: constant.ChannelTypeJimengDimensio},
		{name: "Xinghe", channelType: constant.ChannelTypeXingheVideo},
		{name: "AGGC", channelType: constant.ChannelTypeAGGC},
		{name: "Tencent VOD", channelType: constant.ChannelTypeTencentVOD},
		{name: "Axmgc", channelType: constant.ChannelTypeAxmgc},
		{name: "SeventhFrame", channelType: constant.ChannelTypeSeventhFrame},
		{name: "Vertex", channelType: constant.ChannelTypeVertexAi},
		{name: "Vidu", channelType: constant.ChannelTypeVidu},
		{name: "Doubao", channelType: constant.ChannelTypeDoubaoVideo},
		{name: "VolcEngine (Doubao)", channelType: constant.ChannelTypeVolcEngine},
		{name: "OpenAI video task alias", channelType: constant.ChannelTypeOpenAI},
		{
			name:        "Sora",
			channelType: constant.ChannelTypeSora,
			extraMappedModels: []string{
				"ax2.0-9tu",
				"sdquan-2",
				"otoy-image-to-video-seedance-2-0-mini-reference-to-video",
				"veo-omni-flash",
				"veo-omni-flash-video-edit",
				"grok-video-3",
				"grok-imagine-video-1.5-preview",
				"navos-local-seedance-154-36-180-7",
				"seedance-2-0-15s-slow",
				"seedance-2-0-15s-high",
				"seedance-2-0-15s-fast",
				"seedance-2-0-sale",
				"doubao-seedance-2-0-260128",
				"doubao-seedance-2-0-fast-260128",
				"seedance-2.0-480p-mini-15s",
				"seedance-2.0-480p-fast-15s",
				"seedance-2.0-480p-15s",
				"seedance-2.0-720p-mini-15s",
				"seedance-2.0-720p-fast-15s",
				"seedance-2.0-720p-pro-15s",
				"seedance-2.0-1080p-15s",
				"seedance-2.0-4k-15s",
				"seedance-2-0",
			},
		},
		{
			name:              "Shishi",
			channelType:       constant.ChannelTypeShishi,
			extraMappedModels: []string{"veo-omni-flash", "veo-omni-flash-video-edit"},
		},
		{name: "Gemini", channelType: constant.ChannelTypeGemini},
		{name: "Hailuo", channelType: constant.ChannelTypeMiniMax},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(testCase.channelType)))
			require.NotNil(t, adaptor)
			capabilityProvider, ok := adaptor.(channel.TaskBillingCapabilityProvider)
			require.True(t, ok, "%s does not implement TaskBillingCapabilityProvider", testCase.name)
			_, ok = adaptor.(channel.TaskBillingInputProvider)
			require.True(t, ok, "%s does not implement TaskBillingInputProvider", testCase.name)

			models := append([]string(nil), adaptor.GetModelList()...)
			models = append(models, testCase.extraMappedModels...)
			require.NotEmpty(t, models)
			for _, modelName := range models {
				info := &relaycommon.RelayInfo{
					OriginModelName: modelName,
					ChannelMeta: &relaycommon.ChannelMeta{
						ChannelType:       testCase.channelType,
						UpstreamModelName: modelName,
					},
					TaskRelayInfo: &relaycommon.TaskRelayInfo{},
				}
				capability := normalizeTaskBillingCapability(capabilityProvider.GetTaskBillingCapability(info))
				require.NotNil(t, capability, "%s model %s has no canonical billing capability", testCase.name, modelName)
				require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(CanonicalBillingFields(capability)), "%s model %s returned an invalid canonical billing schema", testCase.name, modelName)
			}
		})
	}
}
