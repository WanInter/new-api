package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestDoubaoVideoUsesOpenAIVideoEndpoint(t *testing.T) {
	endpointTypes := GetEndpointTypesByChannelType(constant.ChannelTypeDoubaoVideo, "bytefor-2.0-fast")
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, endpointTypes)
}

func TestShishiUsesOpenAIVideoEndpoint(t *testing.T) {
	endpointTypes := GetEndpointTypesByChannelType(constant.ChannelTypeShishi, "sd-720-pro")
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, endpointTypes)
}
