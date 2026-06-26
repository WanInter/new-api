package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestAGGCBalanceResponseAcceptsStringCredits(t *testing.T) {
	body := []byte(`{"code":0,"message":"OK","data":{"credits":"10000.5","frozen_credits":"500.25","usage_count":42,"total_spent":"3500"}}`)
	var response AGGCBalanceResponse
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, 9500.25, response.Data.Credits.Float64()-response.Data.FrozenCredits.Float64())
}

func TestAGGCBalanceResponseAcceptsNumericCredits(t *testing.T) {
	body := []byte(`{"code":0,"message":"OK","data":{"credits":10000,"frozen_credits":500,"usage_count":42,"total_spent":3500}}`)
	var response AGGCBalanceResponse
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, 9500.0, response.Data.Credits.Float64()-response.Data.FrozenCredits.Float64())
}
