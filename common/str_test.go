package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanUserVisibleErrorMessageRemovesWaninterRequestPrefix(t *testing.T) {
	t.Parallel()

	input := `waninter request failed with status 400: {"code":"model_price_error","message":"model price not configured"}`

	got := CleanUserVisibleErrorMessage(input)

	require.Equal(t, `request failed with status 400: {"code":"model_price_error","message":"model price not configured"}`, got)
	require.NotContains(t, strings.ToLower(got), "waninter")
}

func TestCleanUserVisibleErrorMessageHandlesCaseAndStatusCodeVariant(t *testing.T) {
	t.Parallel()

	got := CleanUserVisibleErrorMessage("Waninter request failed with status code 500: upstream error")

	require.Equal(t, "request failed with status code 500: upstream error", got)
	require.NotContains(t, strings.ToLower(got), "waninter")
}

func TestCleanUserVisibleErrorMessageMasksWaninterDomain(t *testing.T) {
	t.Parallel()

	got := CleanUserVisibleErrorMessage("request failed: https://api.waninter.com/v1/image/generations?key=secret")

	require.NotContains(t, strings.ToLower(got), "waninter")
	require.Contains(t, got, "https://***.com")
	require.Contains(t, got, "key=***")
}

func TestCleanUserVisibleErrorMessageRemovesPlainWaninterBrand(t *testing.T) {
	t.Parallel()

	got := CleanUserVisibleErrorMessage("waninter upstream returned invalid response")

	require.Equal(t, "upstream upstream returned invalid response", got)
	require.NotContains(t, strings.ToLower(got), "waninter")
}
