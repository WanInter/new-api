package seventhframe

import (
	"bytes"
	"io"
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

func TestSeventhFrameCanonicalBillingJSONMatchesOutgoingDuration(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want int
	}{
		{name: "duration", body: `{"model":"alias","prompt":"animate","duration":"15 seconds"}`, want: 15},
		{name: "seconds alias", body: `{"model":"alias","prompt":"animate","seconds":"12s"}`, want: 12},
		{name: "duration precedence", body: `{"model":"alias","prompt":"animate","duration":10,"seconds":6}`, want: 10},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newSeventhFrameBillingContext(t, "application/json", strings.NewReader(testCase.body))
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: "alias", Prompt: "animate"})
			info := seventhFrameBillingInfo(ModelList[0])
			adaptor := &TaskAdaptor{channel: "channel14"}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, seventhFrameCanonicalDuration(t, input.Body))
			require.NoError(t, billingexpr.ValidateCanonicalBillingInput(input.Body, seventhFrameCanonicalFields(t, info)))

			requestBody, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			var upstream generationRequest
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream.Duration)
			require.True(t, ok)
			assert.Equal(t, testCase.want, wireSeconds)
		})
	}
}

func TestSeventhFrameCanonicalBillingFormsMatchOutgoingDuration(t *testing.T) {
	testCases := []struct {
		name string
		body func(t *testing.T) (string, []byte)
	}{
		{
			name: "urlencoded",
			body: func(_ *testing.T) (string, []byte) {
				return "application/x-www-form-urlencoded", []byte("model=alias&prompt=animate&duration=11s&seconds=6")
			},
		},
		{
			name: "multipart",
			body: func(t *testing.T) (string, []byte) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "alias"))
				require.NoError(t, writer.WriteField("prompt", "animate"))
				require.NoError(t, writer.WriteField("duration", "11 sec"))
				require.NoError(t, writer.WriteField("seconds", "6"))
				require.NoError(t, writer.Close())
				return writer.FormDataContentType(), body.Bytes()
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contentType, body := testCase.body(t)
			c := newSeventhFrameBillingContext(t, contentType, bytes.NewReader(body))
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: "alias", Prompt: "animate"})
			info := seventhFrameBillingInfo(ModelList[1])
			adaptor := &TaskAdaptor{channel: "channel14"}

			input, err := adaptor.BuildBillingInput(c, info)
			require.NoError(t, err)
			assert.Equal(t, 11, seventhFrameCanonicalDuration(t, input.Body))

			requestBody, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(requestBody)
			require.NoError(t, err)
			var upstream generationRequest
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			wireSeconds, ok := taskcommon.PositiveIntegerSecondsFromWireValue(upstream.Duration)
			require.True(t, ok)
			assert.Equal(t, 11, wireSeconds)
		})
	}
}

func TestSeventhFrameCanonicalBillingRejectsUnknownDurationThroughSchema(t *testing.T) {
	for _, body := range []string{
		`{"model":"alias","prompt":"animate"}`,
		`{"model":"alias","prompt":"animate","duration":0}`,
		`{"model":"alias","prompt":"animate","duration":4.5}`,
		`{"model":"alias","prompt":"animate","duration":"invalid"}`,
	} {
		c := newSeventhFrameBillingContext(t, "application/json", strings.NewReader(body))
		c.Set("task_request", relaycommon.TaskSubmitReq{Model: "alias", Prompt: "animate"})
		info := seventhFrameBillingInfo(ModelList[0])
		input, err := (&TaskAdaptor{}).BuildBillingInput(c, info)
		require.NoError(t, err)
		assert.Error(t, billingexpr.ValidateCanonicalBillingInput(input.Body, seventhFrameCanonicalFields(t, info)))
	}
}

func TestSeventhFrameCanonicalBillingCapabilityModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	for _, modelName := range ModelList {
		capability := adaptor.GetTaskBillingCapability(seventhFrameBillingInfo(modelName))
		require.NotNil(t, capability)
		assert.Equal(t, taskcommon.ExplicitDurationBillingSchema4To15, capability.SchemaVersion)
	}
	assert.Nil(t, adaptor.GetTaskBillingCapability(seventhFrameBillingInfo("unknown-model")))
}

func newSeventhFrameBillingContext(t *testing.T, contentType string, body io.Reader) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	c.Request.Header.Set("Content-Type", contentType)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}

func seventhFrameBillingInfo(modelName string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "public-seedance",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: modelName},
	}
}

func seventhFrameCanonicalDuration(t *testing.T, body []byte) int {
	t.Helper()
	var input struct {
		Billing struct {
			DurationSeconds int `json:"duration_seconds"`
		} `json:"billing"`
	}
	require.NoError(t, common.Unmarshal(body, &input))
	return input.Billing.DurationSeconds
}

func seventhFrameCanonicalFields(t *testing.T, info *relaycommon.RelayInfo) []billingexpr.CanonicalBillingField {
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
