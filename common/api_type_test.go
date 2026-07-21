package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestShishiUsesOpenAIAPIType(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeShishi)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
}
