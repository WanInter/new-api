package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestChannelValidateSettingsSeventhFrameChannel(t *testing.T) {
	testCases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "uses default channel14 when query is absent",
			baseURL: "https://diqizhen.jytt4.cn/api/v1",
		},
		{
			name:    "accepts channel17 query",
			baseURL: "https://diqizhen.jytt4.cn/api/v1?channel=channel17",
		},
		{
			name:    "rejects channel outside supported range",
			baseURL: "https://diqizhen.jytt4.cn/api/v1?channel=channel18",
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			channel := Channel{
				Type:    constant.ChannelTypeSeventhFrame,
				BaseURL: &testCase.baseURL,
			}

			if testCase.wantErr {
				require.Error(t, channel.ValidateSettings())
				return
			}
			require.NoError(t, channel.ValidateSettings())
		})
	}
}
