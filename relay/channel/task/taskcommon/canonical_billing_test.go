package taskcommon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurationOnlyBillingCapabilityAndInput(t *testing.T) {
	capability := DurationOnlyBillingCapability("video.duration.explicit.v1", 4, 6)
	require.NotNil(t, capability)
	require.Len(t, capability.Fields, 1)
	assert.Equal(t, []string{"4", "5", "6"}, capability.Fields[0].EnumValues)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("X-Request", "current")
	input, err := BuildCanonicalBillingInput(c, &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{"X-Frozen": "frozen"},
	}, map[string]any{"duration_seconds": 5})
	require.NoError(t, err)
	assert.Equal(t, "current", input.Headers["X-Request"])
	assert.Equal(t, "frozen", input.Headers["X-Frozen"])

	fields := []billingexpr.CanonicalBillingField{
		{
			Path:       capability.Fields[0].Path,
			Type:       capability.Fields[0].Type,
			Required:   capability.Fields[0].Required,
			EnumValues: capability.Fields[0].EnumValues,
		},
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(fields))
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, fields))
}

func TestPositiveIntegerSecondsFromWireValue(t *testing.T) {
	testCases := []struct {
		name  string
		value any
		want  int
		ok    bool
	}{
		{name: "integer", value: 15, want: 15, ok: true},
		{name: "json number", value: json.Number("12"), want: 12, ok: true},
		{name: "integral float", value: 8.0, want: 8, ok: true},
		{name: "numeric string", value: " 10 ", want: 10, ok: true},
		{name: "short suffix", value: "6s", want: 6, ok: true},
		{name: "word suffix", value: " 5 Seconds ", want: 5, ok: true},
		{name: "raw number", value: json.RawMessage(`14`), want: 14, ok: true},
		{name: "raw string", value: json.RawMessage(`"9 sec"`), want: 9, ok: true},
		{name: "zero", value: 0},
		{name: "negative", value: -4},
		{name: "fractional number", value: 4.5},
		{name: "fractional string", value: "4.5s"},
		{name: "garbage", value: "soon"},
		{name: "object", value: map[string]any{"seconds": 5}},
		{name: "nil", value: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := PositiveIntegerSecondsFromWireValue(testCase.value)
			assert.Equal(t, testCase.ok, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestDurationSizeBillingCapability(t *testing.T) {
	capability := DurationSizeBillingCapability(ExplicitDurationSizeBillingSchema4To15, 4, 15)
	require.Len(t, capability.Fields, 2)
	assert.Equal(t, CanonicalDurationSecondsPath, capability.Fields[0].Path)
	assert.Equal(t, CanonicalSizePath, capability.Fields[1].Path)
	assert.Equal(t, CanonicalOpenAIVideoSizes(), capability.Fields[1].EnumValues)
	assert.True(t, capability.Fields[1].Required)
}

func TestDurationValuesStringBillingCapability(t *testing.T) {
	capability := DurationValuesStringBillingCapability(
		"video.duration.discrete.resolution.v1",
		[]int{5, 10, 15},
		CanonicalResolutionPath,
		[]string{"720p", "1080p"},
	)
	require.Len(t, capability.Fields, 2)
	assert.Equal(t, []string{"5", "10", "15"}, capability.Fields[0].EnumValues)
	assert.Equal(t, CanonicalResolutionPath, capability.Fields[1].Path)
	assert.Equal(t, []string{"720p", "1080p"}, capability.Fields[1].EnumValues)
}
