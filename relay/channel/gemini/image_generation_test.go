package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestSupportsGeminiImageModels(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash-image",
		},
	}

	converted, err := adaptor.ConvertImageRequest(nil, info, dto.ImageRequest{
		Prompt:  "draw a cat",
		Size:    "1024*1024",
		Quality: "high",
	})
	require.NoError(t, err)

	geminiRequest, ok := converted.(dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiRequest.Contents, 1)
	require.Equal(t, "user", geminiRequest.Contents[0].Role)
	require.Len(t, geminiRequest.Contents[0].Parts, 1)
	require.Equal(t, "draw a cat", geminiRequest.Contents[0].Parts[0].Text)
	require.Equal(t, []string{"IMAGE"}, geminiRequest.GenerationConfig.ResponseModalities)

	var imageConfig map[string]string
	require.NoError(t, common.Unmarshal(geminiRequest.GenerationConfig.ImageConfig, &imageConfig))
	require.Equal(t, "1:1", imageConfig["aspectRatio"])
	require.Equal(t, "2K", imageConfig["imageSize"])
}

func TestConvertImageRequestUsesParametersForGeminiImageModels(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash-image",
		},
	}

	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model": "nano-banana-2",
		"prompt": "draw a cat",
		"parameters": {
			"size": "1024*1792",
			"n": 2,
			"quality": "high"
		}
	}`), &request))

	converted, err := adaptor.ConvertImageRequest(nil, info, request)
	require.NoError(t, err)

	geminiRequest, ok := converted.(dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotNil(t, geminiRequest.GenerationConfig.CandidateCount)
	require.Equal(t, 2, *geminiRequest.GenerationConfig.CandidateCount)

	var imageConfig map[string]string
	require.NoError(t, common.Unmarshal(geminiRequest.GenerationConfig.ImageConfig, &imageConfig))
	require.Equal(t, "9:16", imageConfig["aspectRatio"])
	require.Equal(t, "2K", imageConfig["imageSize"])
}

func TestGeminiGenerateContentImageHandlerReturnsOpenAIImageResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	body, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Parts: []dto.GeminiPart{
						{Text: "ignored"},
						{InlineData: &dto.GeminiInlineData{
							MimeType: "image/png",
							Data:     "aW1hZ2U=",
						}},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     5,
			CandidatesTokenCount: 10,
			TotalTokenCount:      15,
		},
	})
	require.NoError(t, err)

	usage, newAPIError := GeminiGenerateContentImageHandler(
		c,
		&relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations},
		&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.PromptTokens)
	require.Equal(t, 10, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)

	var imageResponse dto.ImageResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &imageResponse))
	require.Len(t, imageResponse.Data, 1)
	require.Equal(t, "aW1hZ2U=", imageResponse.Data[0].B64Json)
}
