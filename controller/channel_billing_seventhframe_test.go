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
	testCases := []struct {
		name      string
		baseURL   func(string) string
		wantQuery string
	}{
		{
			name:      "defaults to channel14",
			baseURL:   func(serverURL string) string { return serverURL + "/" },
			wantQuery: "channel=channel14",
		},
		{
			name: "uses channel from base URL query",
			baseURL: func(serverURL string) string {
				return serverURL + "/?channel=channel17"
			},
			wantQuery: "channel=channel17",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
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

			baseURL := testCase.baseURL(server.URL)
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
			assert.Equal(t, testCase.wantQuery, request.query)
			assert.Equal(t, "Bearer seventhframe-test-key", request.authorization)

			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, 970.0, stored.Balance)
			assert.NotZero(t, stored.BalanceUpdatedTime)
			assert.Equal(t, model.ChannelBalanceStatusAvailable, stored.BalanceStatus)
		})
	}
}
