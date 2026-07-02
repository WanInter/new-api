package controller

import (
	"testing"

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
