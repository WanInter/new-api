package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveModelMapping(t *testing.T) {
	testCases := []struct {
		name      string
		mapping   string
		origin    string
		model     string
		mapped    bool
		chain     []string
		wantError string
	}{
		{name: "no mapping", mapping: "{}", origin: "sd-bak-1", model: "sd-bak-1", chain: []string{"sd-bak-1"}},
		{name: "chain", mapping: `{"sd-bak-1":"alias-2","alias-2":"ax2.0-9tu"}`, origin: "sd-bak-1", model: "ax2.0-9tu", mapped: true, chain: []string{"sd-bak-1", "alias-2", "ax2.0-9tu"}},
		{name: "self mapping", mapping: `{"sd-bak-1":"sd-bak-1"}`, origin: "sd-bak-1", model: "sd-bak-1", chain: []string{"sd-bak-1"}},
		{name: "cycle", mapping: `{"sd-bak-1":"alias-2","alias-2":"sd-bak-1"}`, origin: "sd-bak-1", wantError: "model_mapping_contains_cycle"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := ResolveModelMapping(testCase.mapping, testCase.origin)
			if testCase.wantError != "" {
				require.EqualError(t, err, testCase.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.model, result.Model)
			assert.Equal(t, testCase.mapped, result.Mapped)
			assert.Equal(t, testCase.chain, result.Chain)
		})
	}
}
