package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoHidesInternalModelNames(t *testing.T) {
	task := &model.Task{
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeJimengDimensio)),
		Properties: model.Properties{
			Input:             "prompt",
			UpstreamModelName: "jimeng-video-seedance-2.0-vip",
			OriginModelName:   "Seedance2.0-jimeng",
		},
	}

	taskDto := TaskModel2Dto(task)
	properties, ok := taskDto.Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, "prompt", properties.Input)
	assert.Empty(t, properties.UpstreamModelName)
	assert.Empty(t, properties.OriginModelName)
	assert.Equal(t, "Seedance2.0-jimeng", taskDto.ModelName)

	encoded, err := common.Marshal(taskDto)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "upstream_model_name")
	assert.NotContains(t, string(encoded), "origin_model_name")
	assert.Contains(t, string(encoded), "model_name")

	assert.Equal(t, "jimeng-video-seedance-2.0-vip", task.Properties.UpstreamModelName)
	assert.Equal(t, "Seedance2.0-jimeng", task.Properties.OriginModelName)
}

func TestTaskModel2DtoHidesInternalModelNamesForOtherChannels(t *testing.T) {
	task := &model.Task{
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)),
		Properties: model.Properties{
			UpstreamModelName: "sora-upstream",
			OriginModelName:   "sora-origin",
		},
	}

	taskDto := TaskModel2Dto(task)
	properties, ok := taskDto.Properties.(model.Properties)
	require.True(t, ok)
	assert.Empty(t, properties.UpstreamModelName)
	assert.Empty(t, properties.OriginModelName)
	assert.Equal(t, "sora-origin", taskDto.ModelName)

	encoded, err := common.Marshal(taskDto)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "upstream_model_name")
	assert.NotContains(t, string(encoded), "origin_model_name")
	assert.Contains(t, string(encoded), "model_name")
	assert.Equal(t, "sora-upstream", task.Properties.UpstreamModelName)
	assert.Equal(t, "sora-origin", task.Properties.OriginModelName)
}

func TestShouldApplyTaskOtherRatiosSkipsFixedModelPrice(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			UsePrice: true,
			Quota:    10,
			OtherRatios: map[string]float64{
				"seconds": 15,
			},
		},
	}

	assert.False(t, shouldApplyTaskOtherRatios(info, "grok-image-video"))
}

func TestSanitizeTaskUpstreamErrorReplacesMappedModelName(t *testing.T) {
	body := []byte(`{"message":"Request validation failed for model \"otoy-image-to-video-seedance-2-0-mini-reference-to-video\"."}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "Seedance2.0-cheap",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "otoy-image-to-video-seedance-2-0-mini-reference-to-video",
		},
	}

	got := sanitizeTaskUpstreamError(body, info)

	assert.NotContains(t, got, "otoy-image-to-video-seedance-2-0-mini-reference-to-video")
	assert.Contains(t, got, "Seedance2.0-cheap")
}

func TestShouldApplyTaskOtherRatiosKeepsDynamicRatioBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			UsePrice: false,
			Quota:    10,
			OtherRatios: map[string]float64{
				"seconds": 15,
			},
		},
	}

	assert.True(t, shouldApplyTaskOtherRatios(info, "dynamic-video-model"))
}

func TestShouldApplyTaskOtherRatiosSkipsTieredExpressionBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData:             types.PriceData{UsePrice: false, Quota: 10},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{ModelName: "tiered-video-model"},
	}

	assert.False(t, shouldApplyTaskOtherRatios(info, "tiered-video-model"))
}
