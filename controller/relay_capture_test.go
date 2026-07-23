package controller

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycapture "github.com/QuantumNous/new-api/relay/capture"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type relayCaptureStorageSpy struct {
	saved chan relaycapture.Artifact
}

func (s *relayCaptureStorageSpy) Save(_ context.Context, artifact relaycapture.Artifact) error {
	s.saved <- artifact
	return nil
}

func (s *relayCaptureStorageSpy) List(context.Context, relaycapture.ListFilter) (relaycapture.ListResult, error) {
	return relaycapture.ListResult{}, nil
}

func (s *relayCaptureStorageSpy) Open(context.Context, string, string) (io.ReadCloser, relaycapture.Metadata, error) {
	return nil, relaycapture.Metadata{}, nil
}

func (s *relayCaptureStorageSpy) DeleteBefore(context.Context, int64) (int, error) {
	return 0, nil
}

func (s *relayCaptureStorageSpy) Health(context.Context) error {
	return nil
}

func TestPersistRelayCaptureOnlySavesSuccessfulOutcome(t *testing.T) {
	previousStorage := relaycapture.GetStorage()
	storage := &relayCaptureStorageSpy{saved: make(chan relaycapture.Artifact, 1)}
	relaycapture.SetStorage(storage)
	t.Cleanup(func() { relaycapture.SetStorage(previousStorage) })

	session := relaycapture.NewSession(
		relaycapture.Metadata{ChannelID: 7},
		http.Header{"Content-Type": []string{"application/json"}},
		"application/json",
		[]byte(`{"model":"kimi-k3"}`),
		19,
		false,
	)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kimi-k3"}}

	persistRelayCapture(session, info, http.StatusBadGateway, "error")
	select {
	case artifact := <-storage.saved:
		t.Fatalf("unexpected failed relay capture: %+v", artifact.Metadata)
	default:
	}

	persistRelayCapture(session, info, http.StatusOK, "success")
	select {
	case artifact := <-storage.saved:
		require.Equal(t, "success", artifact.Metadata.Outcome)
		assert.Equal(t, http.StatusOK, artifact.Metadata.StatusCode)
		assert.Equal(t, "kimi-k3", artifact.Metadata.UpstreamModel)
	case <-time.After(time.Second):
		t.Fatal("successful relay capture was not saved")
	}
}

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
