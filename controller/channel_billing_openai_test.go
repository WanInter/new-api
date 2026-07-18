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

func TestUpdateChannelBalanceMarksOpenAIPlaceholderAsUnavailable(t *testing.T) {
	db := setupChannelBillingTestDB(t)
	requestedPaths := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"billing_subscription","has_payment_method":true,"hard_limit_usd":100000000,"system_hard_limit_usd":100000000}`))
	}))
	defer server.Close()

	baseURL := server.URL
	channel := &model.Channel{
		Type:          constant.ChannelTypeOpenAI,
		Key:           "openai-compatible-test-key",
		BaseURL:       &baseURL,
		Balance:       123,
		BalanceStatus: model.ChannelBalanceStatusAvailable,
	}
	require.NoError(t, db.Create(channel).Error)

	balance, err := updateChannelBalance(channel)
	require.NoError(t, err)
	assert.Zero(t, balance)
	assert.Equal(t, "/v1/dashboard/billing/subscription", <-requestedPaths)
	assert.Empty(t, requestedPaths, "placeholder balances must not trigger a usage request")

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Zero(t, stored.Balance)
	assert.NotZero(t, stored.BalanceUpdatedTime)
	assert.Equal(t, model.ChannelBalanceStatusUnavailable, stored.BalanceStatus)

	channel.UpdateBalance(42)
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, 42.0, stored.Balance)
	assert.Equal(t, model.ChannelBalanceStatusAvailable, stored.BalanceStatus)
}
