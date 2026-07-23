package billingexpr_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func canonicalVideoFields() []billingexpr.CanonicalBillingField {
	return []billingexpr.CanonicalBillingField{
		{
			Path:       "billing.duration_seconds",
			Type:       "number",
			Required:   true,
			EnumValues: []string{"4", "15"},
		},
		{
			Path:       "billing.resolution",
			Type:       "string",
			Required:   true,
			EnumValues: []string{"480p", "720p"},
		},
	}
}

func TestValidateCanonicalBillingExpressionMatrix(t *testing.T) {
	expression := `param("billing.resolution") == "480p" ? (param("billing.duration_seconds") == 4 ? tier("480p_4s", 600000) : tier("480p_15s", 2250000)) : (param("billing.duration_seconds") == 4 ? tier("720p_4s", 800000) : tier("720p_15s", 3000000))`

	require.NoError(t, billingexpr.ValidateCanonicalBillingExpressionMatrix(expression, canonicalVideoFields()))
}

func TestValidateCanonicalBillingExpressionRejectsRawRequestAccess(t *testing.T) {
	testCases := []string{
		`tier("raw", param("duration") * 1)`,
		`tier("header", header("x-price") == "yes" ? 1 : 0)`,
		`tier("dynamic", param("billing." + "duration_seconds") * 1)`,
		`tier("unknown", param("billing.quality") == "high" ? 1 : 0)`,
	}

	for _, expression := range testCases {
		t.Run(expression, func(t *testing.T) {
			err := billingexpr.ValidateCanonicalBillingExpression(expression, canonicalVideoFields())
			assert.Error(t, err)
		})
	}
}

func TestValidateCanonicalBillingInputRejectsZeroAndUndeclaredValues(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "zero required duration",
			body: `{"billing":{"duration_seconds":0,"resolution":"720p"}}`,
		},
		{
			name: "string duration does not satisfy number field",
			body: `{"billing":{"duration_seconds":"4","resolution":"720p"}}`,
		},
		{
			name: "unsupported resolution",
			body: `{"billing":{"duration_seconds":4,"resolution":"1080p"}}`,
		},
		{
			name: "undeclared input",
			body: `{"billing":{"duration_seconds":4,"resolution":"720p","raw_duration":"99"}}`,
		},
		{
			name: "non canonical root",
			body: `{"billing":{"duration_seconds":4,"resolution":"720p"},"duration":99}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Error(t, billingexpr.ValidateCanonicalBillingInput([]byte(testCase.body), canonicalVideoFields()))
		})
	}

	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(
		[]byte(`{"billing":{"duration_seconds":4,"resolution":"720p"}}`),
		canonicalVideoFields(),
	))
}
