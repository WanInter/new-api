package taskcommon

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	CanonicalDurationSecondsPath                   = "billing.duration_seconds"
	CanonicalSizePath                              = "billing.size"
	CanonicalResolutionPath                        = "billing.resolution"
	DefaultCanonicalVideoSize                      = "720x1280"
	ExplicitDurationBillingSchema4To15             = "video.duration-seconds.integer.4-15.explicit-required.v1"
	ExplicitDurationSizeBillingSchema4To15         = "video.duration-seconds.integer.4-15.explicit-required.size.openai-4.default-720x1280.v1"
	ExplicitRequiredDurationSizeBillingSchema4To15 = "video.duration-seconds.integer.4-15.explicit-required.size.openai-4.explicit-required.v1"
)

var canonicalOpenAIVideoSizes = []string{
	"720x1280",
	"1280x720",
	"1792x1024",
	"1024x1792",
}

// DurationOnlyBillingCapability builds the common contract used by video
// adaptors that can guarantee one positive integer duration.
func DurationOnlyBillingCapability(schemaVersion string, minSeconds, maxSeconds int) *relaycommon.TaskBillingCapability {
	values := make([]int, 0, maxSeconds-minSeconds+1)
	for seconds := minSeconds; seconds <= maxSeconds; seconds++ {
		values = append(values, seconds)
	}
	return DurationValuesBillingCapability(schemaVersion, values)
}

// DurationValuesBillingCapability declares an exact finite set of billable
// durations. Use it for fixed-duration and discrete-duration provider models.
func DurationValuesBillingCapability(schemaVersion string, secondsValues []int) *relaycommon.TaskBillingCapability {
	values := make([]string, 0, len(secondsValues))
	for _, seconds := range secondsValues {
		values = append(values, strconv.Itoa(seconds))
	}
	return &relaycommon.TaskBillingCapability{
		SchemaVersion: schemaVersion,
		Fields: []relaycommon.TaskBillingField{
			{
				Path:       CanonicalDurationSecondsPath,
				Type:       "number",
				Required:   true,
				EnumValues: values,
			},
		},
	}
}

// DurationSizeBillingCapability adds the finite OpenAI-compatible video size
// dimension used by Sora-style per-request billing.
func DurationSizeBillingCapability(schemaVersion string, minSeconds, maxSeconds int) *relaycommon.TaskBillingCapability {
	return DurationStringBillingCapability(schemaVersion, minSeconds, maxSeconds, CanonicalSizePath, CanonicalOpenAIVideoSizes())
}

// DurationStringBillingCapability adds one required enumerable string
// dimension, such as size or resolution, to an integer duration schema.
func DurationStringBillingCapability(schemaVersion string, minSeconds, maxSeconds int, path string, values []string) *relaycommon.TaskBillingCapability {
	secondsValues := make([]int, 0, maxSeconds-minSeconds+1)
	for seconds := minSeconds; seconds <= maxSeconds; seconds++ {
		secondsValues = append(secondsValues, seconds)
	}
	return DurationValuesStringBillingCapability(schemaVersion, secondsValues, path, values)
}

// DurationValuesStringBillingCapability combines an exact duration set with
// one required enumerable string dimension such as size or resolution.
func DurationValuesStringBillingCapability(schemaVersion string, secondsValues []int, path string, values []string) *relaycommon.TaskBillingCapability {
	capability := DurationValuesBillingCapability(schemaVersion, secondsValues)
	capability.Fields = append(capability.Fields, relaycommon.TaskBillingField{
		Path:       path,
		Type:       "string",
		Required:   true,
		EnumValues: append([]string(nil), values...),
	})
	return capability
}

func CanonicalOpenAIVideoSizes() []string {
	return append([]string(nil), canonicalOpenAIVideoSizes...)
}

// CanonicalBillingHeaders copies the transient request headers used by the
// expression runtime. Canonical input persistence stores only the body.
func CanonicalBillingHeaders(c *gin.Context, info *relaycommon.RelayInfo) map[string]string {
	headers := make(map[string]string)
	if info != nil {
		for key, value := range info.RequestHeaders {
			if strings.TrimSpace(key) != "" {
				headers[key] = value
			}
		}
	}
	if c == nil || c.Request == nil {
		return headers
	}
	for key, values := range c.Request.Header {
		if strings.TrimSpace(key) == "" || len(values) == 0 {
			continue
		}
		headers[key] = strings.Join(values, ",")
	}
	return headers
}

// BuildCanonicalBillingInput serializes the only body shape accepted by a
// schema-pinned billing expression.
func BuildCanonicalBillingInput(c *gin.Context, info *relaycommon.RelayInfo, billing map[string]any) (billingexpr.RequestInput, error) {
	body, err := common.Marshal(map[string]any{"billing": billing})
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	return billingexpr.RequestInput{
		Headers: CanonicalBillingHeaders(c, info),
		Body:    body,
	}, nil
}

// PositiveIntegerSecondsFromWireValue converts an already selected provider
// wire value to integer seconds. It accepts numeric values and common textual
// second suffixes, but never rounds fractions or invents a missing default.
func PositiveIntegerSecondsFromWireValue(value any) (int, bool) {
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		if len(raw) == 0 || common.Unmarshal(raw, &decoded) != nil {
			return 0, false
		}
		return PositiveIntegerSecondsFromWireValue(decoded)
	}

	var seconds float64
	switch typed := value.(type) {
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		for _, suffix := range []string{"seconds", "second", "secs", "sec", "s"} {
			if strings.HasSuffix(text, suffix) {
				text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
				break
			}
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		seconds = parsed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		seconds = parsed
	case int:
		return positiveIntegerSeconds(int64(typed))
	case int8:
		return positiveIntegerSeconds(int64(typed))
	case int16:
		return positiveIntegerSeconds(int64(typed))
	case int32:
		return positiveIntegerSeconds(int64(typed))
	case int64:
		return positiveIntegerSeconds(typed)
	case uint:
		return positiveUnsignedIntegerSeconds(uint64(typed))
	case uint8:
		return positiveUnsignedIntegerSeconds(uint64(typed))
	case uint16:
		return positiveUnsignedIntegerSeconds(uint64(typed))
	case uint32:
		return positiveUnsignedIntegerSeconds(uint64(typed))
	case uint64:
		return positiveUnsignedIntegerSeconds(typed)
	case float32:
		seconds = float64(typed)
	case float64:
		seconds = typed
	default:
		return 0, false
	}

	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || math.Trunc(seconds) != seconds || seconds > float64(maxInt()) {
		return 0, false
	}
	return int(seconds), true
}

func positiveIntegerSeconds(value int64) (int, bool) {
	if value <= 0 || uint64(value) > uint64(maxInt()) {
		return 0, false
	}
	return int(value), true
}

func positiveUnsignedIntegerSeconds(value uint64) (int, bool) {
	if value == 0 || value > uint64(maxInt()) {
		return 0, false
	}
	return int(value), true
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
