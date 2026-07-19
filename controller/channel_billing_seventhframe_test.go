package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelBalanceQueriesSeventhFramePointsBalance(t *testing.T) {
	db := setupChannelBillingTestDB(t)
	type observedRequest struct {
		method        string
		path          string
		query         string
		authorization string
	}
	requestObserved := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestObserved <- observedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			query:         r.URL.RawQuery,
			authorization: r.Header.Get("Authorization"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"usage","channel":"channel14","key":{"pointsBalance":970}}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	channel := &model.Channel{
		Type:    constant.ChannelTypeSeventhFrame,
		Key:     "seventhframe-test-key",
		BaseURL: &baseURL,
	}
	require.NoError(t, db.Create(channel).Error)

	balance, err := updateChannelBalance(channel)
	require.NoError(t, err)
	assert.Equal(t, 970.0, balance)
	request := <-requestObserved
	assert.Equal(t, http.MethodGet, request.method)
	assert.Equal(t, "/usage", request.path)
	assert.Equal(t, "channel=channel14", request.query)
	assert.Equal(t, "Bearer seventhframe-test-key", request.authorization)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, 970.0, stored.Balance)
	assert.NotZero(t, stored.BalanceUpdatedTime)
	assert.Equal(t, model.ChannelBalanceStatusAvailable, stored.BalanceStatus)
}
