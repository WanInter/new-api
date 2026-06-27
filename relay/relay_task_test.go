package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoHidesJimengDimensioUpstreamModelName(t *testing.T) {
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
	assert.Equal(t, "Seedance2.0-jimeng", properties.OriginModelName)

	encoded, err := common.Marshal(taskDto)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "upstream_model_name")
	assert.Contains(t, string(encoded), "origin_model_name")

	assert.Equal(t, "jimeng-video-seedance-2.0-vip", task.Properties.UpstreamModelName)
}

func TestTaskModel2DtoPreservesUpstreamModelNameForOtherChannels(t *testing.T) {
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
	assert.Equal(t, "sora-upstream", properties.UpstreamModelName)
}
