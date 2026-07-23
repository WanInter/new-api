package shishi

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShishiCanonicalBillingJSONMatchesOutgoingDuration(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		want     int
		wantSize string
	}{
		{name: "duration precedence", body: `{"model":"veo-omni-flash","prompt":"animate","duration":15,"seconds":4,"size":"1792x1024"}`, want: 15, wantSize: "1792x1024"},
		{name: "seconds suffix", body: `{"model":"veo-omni-flash","prompt":"animate","seconds":"12 seconds","size":"1280x720"}`, want: 12, wantSize: "1280x720"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newShishiBillingJSONContext(t, testCase.body)
			info := shishiBillingInfo(veoOmniFlashBillingModel)
			adaptor := &TaskAdaptor{}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			canonicalDuration, canonicalSize := shishiCanonicalBilling(t, input.Body)
			assert.Equal(t, testCase.want, canonicalDuration)
			assert.Equal(t, testCase.wantSize, canonicalSize)
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, shishiCanonicalFields(t, info)))

			requestBody, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			var upstream map[string]any
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream["duration"])
			require.True(t, ok)
			assert.Equal(t, testCase.want, wireSeconds)
			assert.Equal(t, testCase.wantSize, upstream["size"])
			assert.NotContains(t, upstream, "seconds")
		})
	}
}

func TestShishiCanonicalBillingMultipartMatchesOutgoingDuration(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", veoOmniFlashBillingModel))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("duration", "11s"))
	require.NoError(t, writer.WriteField("seconds", "6"))
	require.NoError(t, writer.WriteField("size", "1024x1792"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := shishiBillingInfo(veoOmniFlashBillingModel)
	input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
	require.NoError(t, err)
	canonicalDuration, canonicalSize := shishiCanonicalBilling(t, input.Body)
	assert.Equal(t, 11, canonicalDuration)
	assert.Equal(t, "1024x1792", canonicalSize)
	require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, shishiCanonicalFields(t, info)))

	requestBody, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	_, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(encoded), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	assert.Equal(t, []string{"11s"}, form.Value["duration"])
	assert.Equal(t, []string{"1024x1792"}, form.Value["size"])
	assert.NotContains(t, form.Value, "seconds")
}

func TestShishiCanonicalBillingRejectsUnknownDurationThroughSchema(t *testing.T) {
	for _, body := range []string{
		`{"model":"veo-omni-flash","prompt":"animate"}`,
		`{"model":"veo-omni-flash","prompt":"animate","duration":0}`,
		`{"model":"veo-omni-flash","prompt":"animate","duration":4.5}`,
		`{"model":"veo-omni-flash","prompt":"animate","duration":"invalid"}`,
		`{"model":"veo-omni-flash","prompt":"animate","duration":8,"size":"960x540"}`,
	} {
		c := newShishiBillingJSONContext(t, body)
		info := shishiBillingInfo(veoOmniFlashBillingModel)
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, shishiCanonicalFields(t, info)))
	}
}

func TestShishiCanonicalBillingCapabilityModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	for _, modelName := range []string{veoOmniFlashBillingModel, veoOmniFlashVideoEditBillingModel} {
		capability := adaptor.GetTaskBillingCapability(shishiBillingInfo(modelName))
		require.NotNil(t, capability)
		assert.Equal(t, taskcommon.ExplicitDurationSizeBillingSchema4To15, capability.SchemaVersion)
		require.NoError(t, billingexpr.ValidateCanonicalBillingExpressionMatrix(
			`param("billing.size") == "1792x1024" ? (param("billing.duration_seconds") == 4 ? tier("high_4", 2) : tier("high", 3)) : tier("standard", 1)`,
			shishiCanonicalFields(t, shishiBillingInfo(modelName)),
		))
	}
	assert.Nil(t, adaptor.GetTaskBillingCapability(shishiBillingInfo("unknown-model")))
}

const (
	veoOmniFlashBillingModel          = "veo-omni-flash"
	veoOmniFlashVideoEditBillingModel = "veo-omni-flash-video-edit"
)

func newShishiBillingJSONContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func shishiBillingInfo(modelName string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: modelName},
	}
}

func shishiCanonicalBilling(t *testing.T, body []byte) (int, string) {
	t.Helper()
	var input struct {
		Billing struct {
			DurationSeconds int    `json:"duration_seconds"`
			Size            string `json:"size"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(body, &input))
	return input.Billing.DurationSeconds, input.Billing.Size
}

func shishiCanonicalFields(t *testing.T, info *relaycommon.RelayInfo) []billingexpr.CanonicalBillingField {
	t.Helper()
	capability := (&TaskAdaptor{}).GetTaskBillingCapability(info)
	require.NotNil(t, capability)
	fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
	for _, field := range capability.Fields {
		fields = append(fields, billingexpr.CanonicalBillingField{Path: field.Path, Type: field.Type, Required: field.Required, EnumValues: field.EnumValues})
	}
	require.NoError(t, billingexpr.ValidateCanonicalBillingSchema(fields))
	return fields
}
