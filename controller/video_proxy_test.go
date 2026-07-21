package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSameURLHostname(t *testing.T) {
	require.True(t, sameURLHostname(
		"https://api.myaigc.shop/v1/videos/task/content",
		"https://api.myaigc.shop",
	))
	require.True(t, sameURLHostname(
		"https://api.myaigc.shop/v1/videos/task/content",
		"https://api.myaigc.shop/base/path",
	))
	require.False(t, sameURLHostname(
		"https://cdn.example.com/video.mp4",
		"https://api.myaigc.shop",
	))
	require.False(t, sameURLHostname(
		"not a url",
		"https://api.myaigc.shop",
	))
}

func TestExtractVideoURLFromTaskDataReadsNestedYoboxCorpTask(t *testing.T) {
	task := &model.Task{Data: []byte(`{
		"data":{"data":{"task":{
			"outputs":["https://example.com/result.mp4"]
		}}}
	}`)}

	require.Equal(t, "https://example.com/result.mp4", extractVideoURLFromTaskData(task))
}

func TestVideoProxyAPIKeyUsesSelectedShishiTaskKey(t *testing.T) {
	task := &model.Task{PrivateData: model.TaskPrivateData{Key: "selected-key"}}

	require.Equal(t, "selected-key", videoProxyAPIKey(constant.ChannelTypeShishi, task, "key-one\nkey-two"))
	require.Equal(t, "key-one\nkey-two", videoProxyAPIKey(constant.ChannelTypeShishi, nil, "key-one\nkey-two"))
}
