package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelWithVideoModelMapping(t *testing.T, channelType int, upstreamModel string) *model.Channel {
	t.Helper()
	mapping, err := common.Marshal(map[string]string{StrictVideoRoutingModelSDBak1: upstreamModel})
	require.NoError(t, err)
	mappingString := string(mapping)
	return &model.Channel{Type: channelType, ModelMapping: &mappingString}
}

func TestEvaluateChannelVideoRoutingForSDBak1Candidates(t *testing.T) {
	ax := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "ax2.0-9tu")
	sdquan := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "sdquan-2")
	jsonContentType := "application/json"
	duration := 15

	testCases := []struct {
		name           string
		features       VideoRequestFeatures
		axEligible     bool
		sdquanEligible bool
	}{
		{
			name:           "nine image ax boundary",
			features:       VideoRequestFeatures{Images: 9, Duration: &duration, ContentType: jsonContentType},
			axEligible:     true,
			sdquanEligible: false,
		},
		{
			name:           "sdquan multimodal boundary",
			features:       VideoRequestFeatures{Images: 4, Videos: 3, Audios: 1, Duration: &duration, ContentType: jsonContentType},
			axEligible:     false,
			sdquanEligible: true,
		},
		{
			name:           "overlapping image-only request",
			features:       VideoRequestFeatures{Images: 4, Duration: &duration, ContentType: jsonContentType},
			axEligible:     true,
			sdquanEligible: true,
		},
		{
			name:           "wrong duration",
			features:       VideoRequestFeatures{Images: 1, Duration: common.GetPointer(12), ContentType: jsonContentType},
			axEligible:     false,
			sdquanEligible: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			axResult := EvaluateChannelVideoRouting(ax, StrictVideoRoutingModelSDBak1, testCase.features)
			sdquanResult := EvaluateChannelVideoRouting(sdquan, StrictVideoRoutingModelSDBak1, testCase.features)

			assert.Equal(t, testCase.axEligible, axResult.Eligible)
			assert.Equal(t, testCase.sdquanEligible, sdquanResult.Eligible)
			assert.Equal(t, "ax2.0-9tu", axResult.Mapping.Model)
			assert.Equal(t, "sdquan-2", sdquanResult.Mapping.Model)
		})
	}
}

func TestEvaluateChannelVideoRoutingUsesChannelOverride(t *testing.T) {
	maxImages := 6
	settings, err := common.Marshal(dto.ChannelOtherSettings{VideoRouting: &dto.VideoRoutingConfig{
		Models: map[string]dto.VideoModelCapability{
			"*": {Images: &dto.VideoMediaRange{Max: &maxImages}},
		},
	}})
	require.NoError(t, err)
	channel := &model.Channel{Type: constant.ChannelTypeYobox, OtherSettings: string(settings)}

	result := EvaluateChannelVideoRouting(channel, StrictVideoRoutingModelSDBak1, VideoRequestFeatures{Images: 5})

	assert.True(t, result.Eligible)
	assert.Contains(t, result.Sources, "channel_override:*")
}

func TestEvaluateChannelVideoRoutingStrictModelRejectsUnknownCapability(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeSora}

	result := EvaluateChannelVideoRouting(channel, StrictVideoRoutingModelSDBak1, VideoRequestFeatures{})

	assert.False(t, result.Eligible)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "missing_capability", result.Violations[0].Code)
}

func TestGetVideoRequestFeaturesCountsEveryReferenceEntry(t *testing.T) {
	body := `{
		"model":"sd-bak-1",
		"images":["i1","i2"],
		"videos":["v1"],
		"image_urls":"i6",
		"video_url":"v3",
		"audio_url":"a2",
		"content":[
			{"type":"image_url","image_url":{"url":"i2"}},
			{"type":"image_url","image_url":{"url":"i3"}},
			{"type":"video_url","video_url":{"url":"v2"}},
			{"type":"audio_url","audio_url":{"url":"a1"}}
		],
		"input":{"start_frames":["i7"],"image_references":["i4", {"url":"i5"}]},
		"metadata":{"start_frames":["i8"]},
		"duration":"15s"
	}`
	c := newChannelConstraintTestContext(t, "/v1/videos", body)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, 9, features.Images)
	assert.Equal(t, 3, features.Videos)
	assert.Equal(t, 2, features.Audios)
	require.NotNil(t, features.Duration)
	assert.Equal(t, 15, *features.Duration)
}

func TestVideoRoutingUsesExplicitContentForProfiledModels(t *testing.T) {
	body := `{
		"model":"sd-bak-1",
		"images":["1","2","3","4","5","6","7","8","9","10"],
		"content":[
			{"type":"image_url","image_url":{"url":"content.png"}},
			{"type":"text","text":"animate it"}
		]
	}`
	c := newChannelConstraintTestContext(t, "/v1/videos", body)
	features, err := GetVideoRequestFeatures(c)
	require.NoError(t, err)
	assert.Equal(t, 11, features.Images)

	ax := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "ax2.0-9tu")
	sdquan := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "sdquan-2")
	assert.True(t, EvaluateChannelVideoRouting(ax, StrictVideoRoutingModelSDBak1, features).Eligible)
	assert.True(t, EvaluateChannelVideoRouting(sdquan, StrictVideoRoutingModelSDBak1, features).Eligible)
}

func TestVideoRoutingDoesNotDeduplicateRepeatedReferences(t *testing.T) {
	body := `{"model":"sd-bak-1","images":["same.png","same.png","same.png","same.png","same.png","same.png","same.png","same.png","same.png","same.png"]}`
	c := newChannelConstraintTestContext(t, "/v1/videos", body)
	features, err := GetVideoRequestFeatures(c)
	require.NoError(t, err)
	assert.Equal(t, 10, features.Images)

	ax := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "ax2.0-9tu")
	assert.False(t, EvaluateChannelVideoRouting(ax, StrictVideoRoutingModelSDBak1, features).Eligible)
}

func TestVideoRoutingRejectsInvalidExplicitContentForProfiledModels(t *testing.T) {
	body := `{
		"model":"sd-bak-1",
		"prompt":"top-level prompt",
		"content":[{"image_url":{"url":"image.png"}}]
	}`
	c := newChannelConstraintTestContext(t, "/v1/videos", body)
	features, err := GetVideoRequestFeatures(c)
	require.NoError(t, err)

	ax := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "ax2.0-9tu")
	result := EvaluateChannelVideoRouting(ax, StrictVideoRoutingModelSDBak1, features)

	assert.False(t, result.Eligible)
	assert.Contains(t, result.Violations, VideoConstraintViolation{Code: "invalid_content", Field: "content"})
	assert.Contains(t, result.Violations, VideoConstraintViolation{
		Code: "text_below_min", Field: "text", Actual: common.GetPointer(0), Expected: common.GetPointer(1),
	})
}

func TestGetVideoRequestFeaturesReturnsTypedErrorForInvalidMediaField(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"sd-bak-1","images":{"url":"x"}}`)

	_, err := GetVideoRequestFeatures(c)

	var featuresErr *VideoRequestFeaturesError
	require.ErrorAs(t, err, &featuresErr)
	assert.Contains(t, featuresErr.Error(), "task media field")
}

func TestGetVideoRequestFeaturesIgnoresBooleanAudioSwitch(t *testing.T) {
	for _, audio := range []string{"true", "false"} {
		t.Run(audio, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"sd-bak-1","prompt":"animate","audio":`+audio+`}`)

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.Zero(t, features.Audios)
		})
	}
}

func TestGetVideoRequestFeaturesCountsMultipartValuesAndFiles(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for i := 0; i < 4; i++ {
		require.NoError(t, writer.WriteField("images", "https://example.com/"+string(rune('a'+i))+".png"))
	}
	fileWriter, err := writer.CreateFormFile("input_reference", "reference.png")
	require.NoError(t, err)
	_, err = fileWriter.Write([]byte("image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, 5, features.Images)
}

func TestGetVideoRequestFeaturesIgnoresNonVideoRoute(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/images/generations", `{"images":["1"]}`)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, VideoRequestFeatures{}, features)
}

func TestChannelSupportsRequestConstraintsRejectsAxVideoReference(t *testing.T) {
	channel := channelWithVideoModelMapping(t, constant.ChannelTypeSora, "ax2.0-9tu")
	c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"sd-bak-1","videos":["v1"]}`)

	assert.False(t, ChannelSupportsRequestConstraints(c, channel, StrictVideoRoutingModelSDBak1))
}

func TestSimulateVideoRoutingUsesEligiblePriorityTier(t *testing.T) {
	truncate(t)
	group := "creative-video"
	modelName := StrictVideoRoutingModelSDBak1
	axPriority := int64(20)
	sdquanPriority := int64(10)
	yoboxPriority := int64(15)
	aggcPriority := int64(0)
	weight := uint(100)
	axMapping, err := common.Marshal(map[string]string{modelName: "ax2.0-9tu"})
	require.NoError(t, err)
	sdquanMapping, err := common.Marshal(map[string]string{modelName: "sdquan-2"})
	require.NoError(t, err)
	axMappingString := string(axMapping)
	sdquanMappingString := string(sdquanMapping)
	channels := []model.Channel{
		{Id: 101, Name: "AX", Type: constant.ChannelTypeSora, Key: "key", Status: common.ChannelStatusEnabled, Priority: &axPriority, Weight: &weight, ModelMapping: &axMappingString},
		{Id: 102, Name: "SDQuan", Type: constant.ChannelTypeSora, Key: "key", Status: common.ChannelStatusEnabled, Priority: &sdquanPriority, Weight: &weight, ModelMapping: &sdquanMappingString},
		{Id: 103, Name: "Yobox", Type: constant.ChannelTypeYobox, Key: "key", Status: common.ChannelStatusEnabled, Priority: &yoboxPriority, Weight: &weight},
		{Id: 104, Name: "AGGC", Type: constant.ChannelTypeAGGC, Key: "key", Status: common.ChannelStatusEnabled, Priority: &aggcPriority, Weight: &weight},
	}
	require.NoError(t, model.DB.Create(&channels).Error)
	abilities := []model.Ability{
		{Group: group, Model: modelName, ChannelId: 101, Enabled: true, Priority: &axPriority, Weight: weight},
		{Group: group, Model: modelName, ChannelId: 102, Enabled: true, Priority: &sdquanPriority, Weight: weight},
		{Group: group, Model: modelName, ChannelId: 103, Enabled: true, Priority: &yoboxPriority, Weight: weight},
		{Group: group, Model: modelName, ChannelId: 104, Enabled: true, Priority: &aggcPriority, Weight: weight},
	}
	require.NoError(t, model.DB.Create(&abilities).Error)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model:       modelName,
		Group:       group,
		Images:      4,
		Videos:      3,
		Audios:      1,
		Duration:    common.GetPointer(15),
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.NotNil(t, result.TargetPriority)
	assert.Equal(t, sdquanPriority, *result.TargetPriority)
	selectedIDs := make([]int, 0)
	for _, candidate := range result.Candidates {
		if candidate.SelectedPriority {
			selectedIDs = append(selectedIDs, candidate.ChannelID)
		}
	}
	assert.Equal(t, []int{102}, selectedIDs)
}

func TestSimulateVideoRoutingRejectsAdvancedCustomWithoutVideoPath(t *testing.T) {
	truncate(t)
	group := "creative-video"
	modelName := StrictVideoRoutingModelSDBak1
	priority := int64(20)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{modelName: "ax2.0-9tu"})
	require.NoError(t, err)
	otherSettings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		}}},
	})
	require.NoError(t, err)
	mappingString := string(mapping)
	channel := model.Channel{
		Id: 201, Name: "Chat only", Type: constant.ChannelTypeAdvancedCustom, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		ModelMapping: &mappingString, OtherSettings: string(otherSettings),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: modelName, Group: group, Images: 1, Duration: common.GetPointer(15),
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	assert.Equal(t, "request_path_not_supported", result.Candidates[0].ConfigurationError)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.False(t, *result.Candidates[0].Eligible)
	assert.Nil(t, result.TargetPriority)
}

func TestVideoRoutingRulesSupportAlternateVideoEntryPoint(t *testing.T) {
	truncate(t)
	group := "creative-video"
	modelName := StrictVideoRoutingModelSDBak1
	priority := int64(20)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{modelName: "ax2.0-9tu"})
	require.NoError(t, err)
	otherSettings, err := common.Marshal(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/video/generations",
			UpstreamPath: "/v1/video/generations",
		}}},
	})
	require.NoError(t, err)
	mappingString := string(mapping)
	channel := model.Channel{
		Id: 202, Name: "Alternate video route", Type: constant.ChannelTypeAdvancedCustom, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		ModelMapping: &mappingString, OtherSettings: string(otherSettings),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)

	overview, err := GetVideoRoutingRuleSet(modelName, group)
	require.NoError(t, err)
	require.Len(t, overview.Candidates, 1)
	assert.Empty(t, overview.Candidates[0].ConfigurationError)

	primaryRules, err := GetVideoRoutingRuleSetForPath(modelName, group, DefaultVideoRoutingRequestPath)
	require.NoError(t, err)
	require.Len(t, primaryRules.Candidates, 1)
	assert.Equal(t, "request_path_not_supported", primaryRules.Candidates[0].ConfigurationError)

	alternateRules, err := GetVideoRoutingRuleSetForPath(modelName, group, "/v1/video/generations")
	require.NoError(t, err)
	require.Len(t, alternateRules.Candidates, 1)
	assert.Empty(t, alternateRules.Candidates[0].ConfigurationError)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: modelName, Group: group, Images: 1, Duration: common.GetPointer(15),
		ContentType: "application/json", RequestPath: "/v1/video/generations",
	})
	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.True(t, *result.Candidates[0].Eligible)
	require.NotNil(t, result.TargetPriority)
	assert.Equal(t, priority, *result.TargetPriority)
}
