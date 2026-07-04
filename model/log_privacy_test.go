package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsRedactsUpstreamRoutingDetails(t *testing.T) {
	logs := []*Log{
		{
			Id:          123,
			ChannelName: "internal-channel",
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info":          map[string]interface{}{"use_channel": []interface{}{1, 2}},
				"audit_info":          map[string]interface{}{"operator": "admin"},
				"stream_status":       "debug",
				"is_model_mapped":     true,
				"upstream_model_name": "gemini-2.5-flash-image",
				"billing_mode":        "per_call",
			}),
		},
	}

	formatUserLogs(logs, 0)

	assert.Equal(t, 1, logs[0].Id)
	assert.Empty(t, logs[0].ChannelName)

	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.NotContains(t, other, "admin_info")
	assert.NotContains(t, other, "audit_info")
	assert.NotContains(t, other, "stream_status")
	assert.NotContains(t, other, "is_model_mapped")
	assert.NotContains(t, other, "upstream_model_name")
	assert.Equal(t, "per_call", other["billing_mode"])
}
