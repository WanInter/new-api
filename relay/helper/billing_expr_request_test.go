package helper

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResolveIncomingBillingExprRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")

	body := []byte(`{"service_tier":"fast"}`)
	ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
	ctx.Set(common.KeyRequestBody, body)

	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{"Content-Type": "application/json"},
	}

	input, err := ResolveIncomingBillingExprRequestInput(ctx, info)
	require.NoError(t, err)
	require.Equal(t, body, input.Body)
	require.Equal(t, "application/json", input.Headers["Content-Type"])
}

func TestBuildIncomingBillingExprRequestInputIgnoresFrozenInput(t *testing.T) {
	body := []byte(`{"duration":5}`)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"duration":15}`),
		},
	}

	resolved, err := ResolveIncomingBillingExprRequestInput(ctx, info)
	require.NoError(t, err)
	assert.Equal(t, float64(15), gjson.GetBytes(resolved.Body, "duration").Float())

	fresh, err := BuildIncomingBillingExprRequestInput(ctx, info)
	require.NoError(t, err)
	assert.Equal(t, float64(5), gjson.GetBytes(fresh.Body, "duration").Float())
}

func TestBuildBillingExprRequestInputFromRequest(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:  "gemini-3.1-pro-preview",
		Stream: lo.ToPtr(true),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hi",
			},
		},
		MaxTokens: lo.ToPtr(uint(3000)),
	}

	input, err := BuildBillingExprRequestInputFromRequest(request, map[string]string{
		"Content-Type": "application/json",
		"X-Test":       "1",
	})
	require.NoError(t, err)
	require.Equal(t, "application/json", input.Headers["Content-Type"])
	require.Equal(t, "1", input.Headers["X-Test"])
	require.True(t, gjson.GetBytes(input.Body, "stream").Bool())
	require.Equal(t, "user", gjson.GetBytes(input.Body, "messages.0.role").String())
	require.Equal(t, float64(3000), gjson.GetBytes(input.Body, "max_tokens").Float())
}

func TestResolveIncomingBillingExprRequestInputFromMultipartImageEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2-vibe-4k"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("size", "2048x3072"))
	require.NoError(t, writer.WriteField("quality", "high"))
	require.NoError(t, writer.WriteField("custom_tier_hint", "preserved"))
	require.NoError(t, writer.WriteField("tag", "first"))
	require.NoError(t, writer.WriteField("tag", "second"))
	filePart, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, writeErr := filePart.Write([]byte("binary image bytes must not enter billing input"))
		return writeErr
	}())
	require.NoError(t, writer.Close())
	originalBody := append([]byte(nil), body.Bytes()...)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(originalBody))
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	request, err := GetAndValidOpenAIImageRequest(ctx, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	assert.Equal(t, "2048x3072", request.Size)
	assert.Equal(t, "high", request.Quality)

	input, err := ResolveIncomingBillingExprRequestInput(ctx, &relaycommon.RelayInfo{
		Request: request,
		RequestHeaders: map[string]string{
			"Content-Type": writer.FormDataContentType(),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "2048x3072", gjson.GetBytes(input.Body, "size").String())
	assert.Equal(t, "high", gjson.GetBytes(input.Body, "quality").String())
	assert.Equal(t, "preserved", gjson.GetBytes(input.Body, "custom_tier_hint").String())
	assert.Equal(t, "first", gjson.GetBytes(input.Body, "tag.0").String())
	assert.Equal(t, "second", gjson.GetBytes(input.Body, "tag.1").String())
	assert.False(t, gjson.GetBytes(input.Body, "image").Exists())
	assert.NotContains(t, string(input.Body), "binary image bytes")

	expr := `param("size") == "2048x3072" && param("quality") == "high" ? tier("4k_high", 400000) : tier("fallback", 100000)`
	cost, trace, err := billingexpr.RunExprWithRequest(expr, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, float64(400000), cost)
	assert.Equal(t, "4k_high", trace.MatchedTier)

	replayedBody, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, originalBody, replayedBody)
}
