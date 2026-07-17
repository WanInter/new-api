package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelConstraintTestContext(t *testing.T, path string, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
	})
	return c
}

func TestNativeGeminiImageTaskChannelConstraints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newChannelConstraintTestContext(t, "/v1/image/generations", `{
		"model":"nano-banana-2",
		"contents":[{"parts":[{"text":"draw a cat"}]}]
	}`)
	generateContentMapping := `{"nano-banana-2":"gemini-3.1-flash-image-preview"}`
	imagenMapping := `{"nano-banana-2":"imagen-4.0-generate-001"}`
	gemini := &model.Channel{Type: constant.ChannelTypeGemini}
	mappedGemini := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &generateContentMapping}
	mappedImagen := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &imagenMapping}
	openAI := &model.Channel{Type: constant.ChannelTypeOpenAI}

	assert.True(t, ChannelSupportsRequestConstraints(c, gemini, "nano-banana-2"))
	assert.True(t, ChannelSupportsRequestConstraints(c, mappedGemini, "nano-banana-2"))
	assert.False(t, ChannelSupportsRequestConstraints(c, mappedImagen, "nano-banana-2"))
	assert.False(t, ChannelSupportsRequestConstraints(c, openAI, "nano-banana-2"))

	filter, err := channelFilterForRequest(c, "nano-banana-2")
	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.True(t, filter(gemini))
	assert.True(t, filter(mappedGemini))
	assert.False(t, filter(mappedImagen))
	assert.False(t, filter(openAI))
}

func TestOpenAIImageTaskChannelConstraintsRemainUnchanged(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/image/generations", `{
		"model":"gpt-image-1",
		"prompt":"draw a cat"
	}`)
	openAI := &model.Channel{Type: constant.ChannelTypeOpenAI}

	assert.True(t, ChannelSupportsRequestConstraints(c, openAI, "gpt-image-1"))
	filter, err := channelFilterForRequest(c, "gpt-image-1")
	require.NoError(t, err)
	assert.Nil(t, filter)
}

func TestOpenAIStyleGeminiImageReferencesExcludeImagenMappings(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/image/generations", `{
		"model":"nano-banana-2",
		"prompt":"edit the reference",
		"images":[{"image_url":"https://example.com/reference.png"}]
	}`)
	generateContentMapping := `{"nano-banana-2":"gemini-3.1-flash-image-preview"}`
	imagenMapping := `{"nano-banana-2":"imagen-4.0-generate-001"}`
	mappedGemini := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &generateContentMapping}
	mappedImagen := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &imagenMapping}
	vertex := &model.Channel{Type: constant.ChannelTypeVertexAi, ModelMapping: &generateContentMapping}
	openAI := &model.Channel{Type: constant.ChannelTypeOpenAI}

	assert.True(t, ChannelSupportsRequestConstraints(c, mappedGemini, "nano-banana-2"))
	assert.False(t, ChannelSupportsRequestConstraints(c, mappedImagen, "nano-banana-2"))
	assert.False(t, ChannelSupportsRequestConstraints(c, vertex, "nano-banana-2"))
	assert.True(t, ChannelSupportsRequestConstraints(c, openAI, "nano-banana-2"))

	filter, err := channelFilterForRequest(c, "nano-banana-2")
	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.True(t, filter(mappedGemini))
	assert.False(t, filter(mappedImagen))
	assert.False(t, filter(vertex))
	assert.True(t, filter(openAI))
}

func TestOpenAIStyleImageReferenceRoutingDoesNotDependOnContentType(t *testing.T) {
	generateContentMapping := `{"nano-banana-2":"gemini-3.1-flash-image-preview"}`
	imagenMapping := `{"nano-banana-2":"imagen-4.0-generate-001"}`
	mappedGemini := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &generateContentMapping}
	mappedImagen := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &imagenMapping}
	vertex := &model.Channel{Type: constant.ChannelTypeVertexAi, ModelMapping: &generateContentMapping}

	for _, contentType := range []string{"", "application/vnd.api+json"} {
		t.Run(contentType, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/image/generations", `{
				"model":"nano-banana-2",
				"prompt":"edit the reference",
				"images":[{"image_url":"https://example.com/reference.png"}]
			}`)
			if contentType == "" {
				c.Request.Header.Del("Content-Type")
			} else {
				c.Request.Header.Set("Content-Type", contentType)
			}

			assert.True(t, ChannelSupportsRequestConstraints(c, mappedGemini, "nano-banana-2"))
			assert.False(t, ChannelSupportsRequestConstraints(c, mappedImagen, "nano-banana-2"))
			assert.False(t, ChannelSupportsRequestConstraints(c, vertex, "nano-banana-2"))

			filter, err := channelFilterForRequest(c, "nano-banana-2")
			require.NoError(t, err)
			require.NotNil(t, filter)
			assert.True(t, filter(mappedGemini))
			assert.False(t, filter(mappedImagen))
			assert.False(t, filter(vertex))
		})
	}
}

func TestEmptyImageReferenceCollectionsDoNotConstrainChannels(t *testing.T) {
	generateContentMapping := `{"nano-banana-2":"gemini-3.1-flash-image-preview"}`
	imagenMapping := `{"nano-banana-2":"imagen-4.0-generate-001"}`
	mappedImagen := &model.Channel{Type: constant.ChannelTypeGemini, ModelMapping: &imagenMapping}
	vertex := &model.Channel{Type: constant.ChannelTypeVertexAi, ModelMapping: &generateContentMapping}

	for _, body := range []string{
		`{"model":"nano-banana-2","prompt":"draw a cat","images":[ ]}`,
		`{"model":"nano-banana-2","prompt":"draw a cat","image":{ }}`,
	} {
		c := newChannelConstraintTestContext(t, "/v1/image/generations", body)

		assert.True(t, ChannelSupportsRequestConstraints(c, mappedImagen, "nano-banana-2"))
		assert.True(t, ChannelSupportsRequestConstraints(c, vertex, "nano-banana-2"))
		filter, err := channelFilterForRequest(c, "nano-banana-2")
		require.NoError(t, err)
		assert.Nil(t, filter)
	}
}

func TestChannelSupportsRequestConstraintsForYoboxReferenceImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	yobox := &model.Channel{Type: constant.ChannelTypeYobox}
	aggc := &model.Channel{Type: constant.ChannelTypeAGGC}
	testCases := []struct {
		name          string
		path          string
		model         string
		body          string
		yoboxExpected bool
	}{
		{
			name:          "four images remain eligible",
			path:          "/v1/videos",
			body:          `{"images":["1","2","3","4"]}`,
			yoboxExpected: true,
		},
		{
			name:          "five images exclude yobox",
			path:          "/v1/videos",
			body:          `{"images":["1","2","3","4","5"]}`,
			yoboxExpected: false,
		},
		{
			name:          "nine images exclude yobox for default models",
			path:          "/v1/video/generations",
			model:         "seedance-2.0",
			body:          `{"images":["1","2","3","4","5","6","7","8","9"]}`,
			yoboxExpected: false,
		},
		{
			name:          "nine images remain eligible for happy horse 1.1",
			path:          "/v1/videos",
			model:         "happy-horse-1.1",
			body:          `{"images":["1","2","3","4","5","6","7","8","9"]}`,
			yoboxExpected: true,
		},
		{
			name:          "ten images exclude yobox for happy horse 1.1",
			path:          "/v1/videos",
			model:         "happy-horse-1.1",
			body:          `{"images":["1","2","3","4","5","6","7","8","9","10"]}`,
			yoboxExpected: false,
		},
		{
			name: "content images count toward limit",
			path: "/v1/videos",
			body: `{"content":[
				{"image_url":{"url":"1"}},
				{"image_url":{"url":"2"}},
				{"image_url":{"url":"3"}},
				{"image_url":{"url":"4"}},
				{"image_url":{"url":"5"}}
			]}`,
			yoboxExpected: false,
		},
		{
			name:          "non-video route is unaffected",
			path:          "/v1/images/generations",
			body:          `{"images":["1","2","3","4","5"]}`,
			yoboxExpected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, testCase.path, testCase.body)

			assert.Equal(t, testCase.yoboxExpected, ChannelSupportsRequestConstraints(c, yobox, testCase.model))
			assert.True(t, ChannelSupportsRequestConstraints(c, aggc, testCase.model))
		})
	}
}
