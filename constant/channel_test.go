package constant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShishiHasDefaultBaseURL(t *testing.T) {
	require.Greater(t, len(ChannelBaseURLs), ChannelTypeShishi)
	require.Equal(t, "http://154.40.44.244:3000", ChannelBaseURLs[ChannelTypeShishi])
}
