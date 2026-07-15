package common

import (
	"testing"

	common "github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestTaskSubmitReqUnmarshalDurationWithSecondsSuffix(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"prompt":"cat","model":"grok-video-1.5","duration":"15s"}`), &req))
	require.Equal(t, 15, req.Duration)
}

func TestTaskSubmitReqUnmarshalMediaFieldsAcceptsScalarAndArray(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{
		"images":"https://example.com/image.png",
		"videos":["https://example.com/1.mp4","https://example.com/2.mp4"],
		"audios":"https://example.com/audio.mp3"
	}`), &req))

	assert.Equal(t, []string{"https://example.com/image.png"}, req.Images)
	assert.Equal(t, []string{"https://example.com/1.mp4", "https://example.com/2.mp4"}, req.Videos)
	assert.Equal(t, []string{"https://example.com/audio.mp3"}, req.Audios)
}
