package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreserveRelayCapturePolicyForGenericChannelUpdate(t *testing.T) {
	origin := &model.Channel{}
	origin.SetOtherSettings(dto.ChannelOtherSettings{
		RelayCapture: &dto.RelayCapturePolicy{
			Enabled:   true,
			Protocols: []string{dto.RelayCaptureProtocolChatCompletions},
		},
	})

	t.Run("merge omitted policy into other settings", func(t *testing.T) {
		update := &model.Channel{}
		update.SetOtherSettings(dto.ChannelOtherSettings{AzureResponsesVersion: "preview"})

		require.NoError(t, preserveRelayCapturePolicy(origin, update))
		settings := update.GetOtherSettings()
		assert.Equal(t, "preview", settings.AzureResponsesVersion)
		require.NotNil(t, settings.RelayCapture)
		assert.True(t, settings.RelayCapture.Enabled)
		assert.Equal(t, []string{dto.RelayCaptureProtocolChatCompletions}, settings.RelayCapture.Protocols)
	})

	t.Run("preserve omitted settings", func(t *testing.T) {
		update := &model.Channel{}

		require.NoError(t, preserveRelayCapturePolicy(origin, update))
		assert.Equal(t, origin.OtherSettings, update.OtherSettings)
	})

	t.Run("reject policy changes", func(t *testing.T) {
		update := &model.Channel{}
		update.SetOtherSettings(dto.ChannelOtherSettings{
			RelayCapture: &dto.RelayCapturePolicy{
				Enabled:   false,
				Protocols: []string{dto.RelayCaptureProtocolChatCompletions},
			},
		})

		require.Error(t, preserveRelayCapturePolicy(origin, update))
	})
}
