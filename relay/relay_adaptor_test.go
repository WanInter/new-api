package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/common"
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

func TestGetTaskAdaptorAxmgc(t *testing.T) {
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAxmgc))
	adaptor := GetTaskAdaptor(platform)
	require.NotNil(t, adaptor)
	assert.Equal(t, "axmgc", adaptor.GetChannelName())
	assert.Equal(t, []string{"seedance-2-720p-933"}, adaptor.GetModelList())

	provider, ok := adaptor.(taskPrivateDataProvider)
	require.True(t, ok)
	privateData, err := provider.BuildPrivateData(nil, &common.RelayInfo{
		ChannelMeta: &common.ChannelMeta{ApiKey: "hm_selected_key"},
	})
	require.NoError(t, err)
	require.NotNil(t, privateData)
	assert.Equal(t, "hm_selected_key", privateData.Key)
}

func TestGetTaskAdaptorSeventhFrame(t *testing.T) {
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSeventhFrame))
	adaptor := GetTaskAdaptor(platform)

	require.NotNil(t, adaptor)
	assert.Equal(t, "seventh-frame", adaptor.GetChannelName())
	assert.Contains(t, adaptor.GetModelList(), "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78")
	assert.Contains(t, adaptor.GetModelList(), "seedance-2.0--person-stripe--6e9f7f9c--voice-tone--a7f8bf20")
}
