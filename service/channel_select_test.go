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
