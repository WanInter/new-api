package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTaskAdaptorTencentVOD(t *testing.T) {
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeTencentVOD))
	adaptor := GetTaskAdaptor(platform)

	require.NotNil(t, adaptor)
	assert.Equal(t, "tencent-vod", adaptor.GetChannelName())
	assert.Contains(t, adaptor.GetModelList(), "kling-vod-3.0-omni")
}
