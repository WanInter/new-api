package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannelFiltersIncompatibleChannelWithMemoryCache(t *testing.T) {
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	yoboxPriority := int64(10)
	yoboxWeight := uint(10)
	aggcPriority := int64(0)
	aggcWeight := uint(90)

	channelSyncLock.Lock()
	oldGroupMap := group2model2channels
	oldChannels := channelsIDM
	group2model2channels = map[string]map[string][]int{
		"creative-video": {"sd-bak-1": {66, 57}},
	}
	channelsIDM = map[int]*Channel{
		66: {Id: 66, Type: constant.ChannelTypeYobox, Priority: &yoboxPriority, Weight: &yoboxWeight},
		57: {Id: 57, Type: constant.ChannelTypeAGGC, Priority: &aggcPriority, Weight: &aggcWeight},
	}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = oldGroupMap
		channelsIDM = oldChannels
		channelSyncLock.Unlock()
	})

	channel, err := GetRandomSatisfiedChannelWithFilter(
		"creative-video",
		"sd-bak-1",
		0,
		"/v1/videos",
		func(channel *Channel) bool { return channel.Type != constant.ChannelTypeYobox },
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 57, channel.Id)
}

func TestGetRandomSatisfiedChannelFiltersIncompatibleChannelWithoutMemoryCache(t *testing.T) {
	truncateTables(t)
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	yoboxPriority := int64(10)
	yoboxWeight := uint(10)
	aggcPriority := int64(0)
	aggcWeight := uint(90)
	channels := []Channel{
		{Id: 66, Name: "Yobox", Type: constant.ChannelTypeYobox, Key: "yobox-key", Status: common.ChannelStatusEnabled, Priority: &yoboxPriority, Weight: &yoboxWeight},
		{Id: 57, Name: "AGGC", Type: constant.ChannelTypeAGGC, Key: "aggc-key", Status: common.ChannelStatusEnabled, Priority: &aggcPriority, Weight: &aggcWeight},
	}
	require.NoError(t, DB.Create(&channels).Error)
	abilities := []Ability{
		{Group: "creative-video", Model: "sd-bak-1", ChannelId: 66, Enabled: true, Priority: &yoboxPriority, Weight: yoboxWeight},
		{Group: "creative-video", Model: "sd-bak-1", ChannelId: 57, Enabled: true, Priority: &aggcPriority, Weight: aggcWeight},
	}
	require.NoError(t, DB.Create(&abilities).Error)

	channel, err := GetRandomSatisfiedChannelWithFilter(
		"creative-video",
		"sd-bak-1",
		0,
		"/v1/videos",
		func(channel *Channel) bool { return channel.Type != constant.ChannelTypeYobox },
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 57, channel.Id)
}
