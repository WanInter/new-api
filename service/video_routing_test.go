package service

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelWithVideoModelMapping(t *testing.T, id, channelType int, publicModel, upstreamModel string) *model.Channel {
	t.Helper()
	mapping, err := common.Marshal(map[string]string{publicModel: upstreamModel})
	require.NoError(t, err)
	mappingString := string(mapping)
	return &model.Channel{
		Id:           id,
		Name:         fmt.Sprintf("video-channel-%d", id),
		Type:         channelType,
		Key:          "key",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mappingString,
	}
}

func createChannelVideoCapability(t *testing.T, channel *model.Channel, upstreamModel string, capability dto.VideoModelCapability) {
	t.Helper()
	require.NoError(t, model.DB.Create(channel).Error)
	_, err := UpsertChannelVideoRoutingCapabilityRule(channel.Id, upstreamModel, capability, 0, 1)
	require.NoError(t, err)
}

func TestMatchVideoCapabilityResolutions(t *testing.T) {
	capability := dto.VideoModelCapability{Resolutions: []string{"720p", "4k"}}

	assert.Empty(t, MatchVideoCapability(VideoRequestFeatures{Resolution: "4k"}, capability))
	assert.Empty(t, MatchVideoCapability(VideoRequestFeatures{}, capability))
	assert.Contains(t, MatchVideoCapability(VideoRequestFeatures{Resolution: "1080p"}, capability), VideoConstraintViolation{
		Code:                 "resolution_not_supported",
		Field:                "resolution",
		Resolution:           "1080p",
		SupportedResolutions: []string{"720p", "4k"},
	})
}

func TestMatchVideoCapabilityDurations(t *testing.T) {
	capability := dto.VideoModelCapability{Durations: []int{6, 10, 12, 16, 20}}

	assert.Empty(t, MatchVideoCapability(VideoRequestFeatures{Duration: common.GetPointer(20)}, capability))
	assert.Empty(t, MatchVideoCapability(VideoRequestFeatures{}, capability))
	assert.Empty(t, MatchVideoCapability(VideoRequestFeatures{Duration: common.GetPointer(99)}, dto.VideoModelCapability{}))
	assert.Contains(t, MatchVideoCapability(VideoRequestFeatures{Duration: common.GetPointer(15)}, capability), VideoConstraintViolation{
		Code:               "duration_not_supported",
		Field:              "duration",
		Actual:             common.GetPointer(15),
		SupportedDurations: []int{6, 10, 12, 16, 20},
	})
}

func TestExactChannelModelCapabilityDoesNotLeakAcrossChannels(t *testing.T) {
	truncate(t)
	publicModel := "seedance-public"
	upstreamModel := "seedance-2.0-fast-noface"
	noFace := channelWithVideoModelMapping(t, 78, constant.ChannelTypeYobox, publicModel, upstreamModel)
	createChannelVideoCapability(t, noFace, upstreamModel, dto.VideoModelCapability{
		Images:          &dto.VideoMediaRange{Max: common.GetPointer(9)},
		Videos:          &dto.VideoMediaRange{Max: common.GetPointer(3)},
		Audios:          &dto.VideoMediaRange{Max: common.GetPointer(3)},
		VideoAudioTotal: &dto.VideoMediaRange{Max: common.GetPointer(3)},
	})
	standard := channelWithVideoModelMapping(t, 79, constant.ChannelTypeYobox, publicModel, upstreamModel)
	require.NoError(t, model.DB.Create(standard).Error)

	features := VideoRequestFeatures{Images: 9, Videos: 2, Audios: 2}
	noFaceResult := EvaluateChannelVideoRouting(noFace, publicModel, features)
	standardResult := EvaluateChannelVideoRouting(standard, publicModel, features)

	assert.False(t, noFaceResult.Eligible)
	assert.Contains(t, noFaceResult.Violations, VideoConstraintViolation{
		Code: "video_audio_total_above_max", Field: "video_audio_total", Actual: common.GetPointer(4), Expected: common.GetPointer(3),
	})
	require.Len(t, noFaceResult.Sources, 1)
	assert.True(t, strings.HasPrefix(noFaceResult.Sources[0], "database:channel_model#"))
	assert.True(t, standardResult.Eligible)
	assert.Nil(t, standardResult.Capability)
}

func TestReloadVideoRoutingIgnoresLegacyCapabilityScopes(t *testing.T) {
	truncate(t)
	publicModel := "legacy-scope-public"
	upstreamModel := "legacy-scope-upstream"
	channel := channelWithVideoModelMapping(t, 88, constant.ChannelTypeYobox, publicModel, upstreamModel)
	createChannelVideoCapability(t, channel, upstreamModel, dto.VideoModelCapability{
		Images: &dto.VideoMediaRange{Max: common.GetPointer(0)},
	})

	require.NoError(t, model.DB.Create(&model.VideoRoutingCapabilityRule{
		Scope:         model.VideoRoutingScopeUpstreamModel,
		UpstreamModel: upstreamModel,
		Capability:    "not valid JSON",
	}).Error)
	require.NoError(t, ReloadVideoRoutingRuleCache())

	result := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{Images: 1})
	assert.False(t, result.Eligible)
	assert.Contains(t, result.Violations, VideoConstraintViolation{
		Code: "images_above_max", Field: "images", Actual: common.GetPointer(1), Expected: common.GetPointer(0),
	})
}

func TestLegacyChannelVideoRoutingSettingIsIgnored(t *testing.T) {
	truncate(t)
	publicModel := "legacy-channel-setting-public"
	upstreamModel := "legacy-channel-setting-upstream"
	legacySettings, err := common.Marshal(dto.ChannelOtherSettings{
		VideoRouting: &dto.VideoRoutingConfig{Models: map[string]dto.VideoModelCapability{
			"*": {Images: &dto.VideoMediaRange{Max: common.GetPointer(0)}},
		}},
	})
	require.NoError(t, err)
	channel := channelWithVideoModelMapping(t, 89, constant.ChannelTypeYobox, publicModel, upstreamModel)
	channel.OtherSettings = string(legacySettings)
	require.NoError(t, channel.ValidateSettings())
	require.NoError(t, model.DB.Create(channel).Error)

	result := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{Images: 1})
	assert.True(t, result.Eligible)
	assert.Nil(t, result.Capability)
}

func TestVideoRoutingWithoutExactRulePassesUnlessStrict(t *testing.T) {
	truncate(t)
	publicModel := "unconfigured-public-model"
	channel := channelWithVideoModelMapping(t, 80, constant.ChannelTypeYobox, publicModel, "unconfigured-upstream-model")
	require.NoError(t, model.DB.Create(channel).Error)

	result := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{Images: 99, Videos: 99, Audios: 99})
	assert.True(t, result.Eligible)
	assert.Nil(t, result.Capability)

	_, err := UpsertVideoRoutingPolicy(publicModel, true, 0, 1)
	require.NoError(t, err)
	result = EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{})
	assert.False(t, result.Eligible)
	require.Equal(t, []VideoConstraintViolation{{Code: "missing_capability"}}, result.Violations)
}

func TestExactCapabilityPreservesExplicitZeroAndFalse(t *testing.T) {
	truncate(t)
	publicModel := "zero-values-public"
	channel := channelWithVideoModelMapping(t, 81, constant.ChannelTypeYobox, publicModel, "zero-values-upstream")
	ruleCapability := dto.VideoModelCapability{
		Images:      &dto.VideoMediaRange{Max: common.GetPointer(0)},
		RequireJSON: common.GetPointer(false),
	}
	createChannelVideoCapability(t, channel, "zero-values-upstream", ruleCapability)

	result := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{ContentType: "text/plain"})
	require.NotNil(t, result.Capability)
	require.NotNil(t, result.Capability.Images)
	assert.Equal(t, common.GetPointer(0), result.Capability.Images.Max)
	assert.Equal(t, common.GetPointer(false), result.Capability.RequireJSON)
	assert.True(t, result.Eligible)
}

func TestVideoRoutingUsesContentPrecedenceFromExactRule(t *testing.T) {
	truncate(t)
	publicModel := "content-public"
	channel := channelWithVideoModelMapping(t, 82, constant.ChannelTypeShishi, publicModel, "content-upstream")
	createChannelVideoCapability(t, channel, "content-upstream", dto.VideoModelCapability{
		Images:            &dto.VideoMediaRange{Max: common.GetPointer(9)},
		ContentPrecedence: common.GetPointer(true),
		RequireText:       common.GetPointer(true),
	})

	body := `{
		"model":"content-public",
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

	result := EvaluateChannelVideoRouting(channel, publicModel, features)
	assert.True(t, result.Eligible)
	assert.Empty(t, result.Violations)
}

func TestVideoRoutingRejectsInvalidContentWhenExactRuleRequiresIt(t *testing.T) {
	truncate(t)
	publicModel := "content-public"
	channel := channelWithVideoModelMapping(t, 83, constant.ChannelTypeShishi, publicModel, "content-upstream")
	createChannelVideoCapability(t, channel, "content-upstream", dto.VideoModelCapability{
		ContentPrecedence: common.GetPointer(true),
		RequireText:       common.GetPointer(true),
	})

	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"content-public",
		"prompt":"top-level prompt",
		"content":[{"image_url":{"url":"image.png"}}]
	}`)
	features, err := GetVideoRequestFeatures(c)
	require.NoError(t, err)

	result := EvaluateChannelVideoRouting(channel, publicModel, features)
	assert.False(t, result.Eligible)
	assert.Contains(t, result.Violations, VideoConstraintViolation{Code: "invalid_content", Field: "content"})
	assert.Contains(t, result.Violations, VideoConstraintViolation{
		Code: "text_below_min", Field: "text", Actual: common.GetPointer(0), Expected: common.GetPointer(1),
	})
}

func TestGetVideoRequestFeaturesCountsEveryReferenceEntry(t *testing.T) {
	body := `{
		"model":"video-model",
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

func TestGetVideoRequestFeaturesNormalizesDurationAliases(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		expected *int
	}{
		{name: "seconds string", body: `{"model":"video-model","seconds":"20"}`, expected: common.GetPointer(20)},
		{name: "metadata object", body: `{"model":"video-model","metadata":{"duration":"12s"}}`, expected: common.GetPointer(12)},
		{name: "encoded metadata object", body: `{"model":"video-model","metadata":"{\"duration\":\"12s\"}"}`, expected: common.GetPointer(12)},
		{
			name:     "equal aliases",
			body:     `{"model":"video-model","duration":12,"seconds":"12","metadata":{"duration":"12s"}}`,
			expected: common.GetPointer(12),
		},
		{name: "null aliases", body: `{"model":"video-model","duration":null,"seconds":null,"metadata":{"duration":null}}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", testCase.body)

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, features.Duration)
		})
	}
}

func TestGetVideoRequestFeaturesMarksInvalidOrConflictingDurationAliasesForRouting(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		expected *int
	}{
		{
			name:     "duration conflicts with seconds",
			body:     `{"model":"video-model","duration":10,"seconds":"20"}`,
			expected: common.GetPointer(10),
		},
		{
			name:     "seconds conflicts with metadata duration",
			body:     `{"model":"video-model","seconds":"20","metadata":{"duration":12}}`,
			expected: common.GetPointer(20),
		},
		{
			name:     "duration conflicts with metadata duration",
			body:     `{"model":"video-model","duration":10,"metadata":{"duration":"12s"}}`,
			expected: common.GetPointer(10),
		},
		{
			name:     "invalid seconds is not ignored",
			body:     `{"model":"video-model","duration":10,"seconds":"10.5"}`,
			expected: common.GetPointer(10),
		},
		{
			name: "invalid metadata duration",
			body: `{"model":"video-model","metadata":{"duration":0}}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", testCase.body)

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, features.Duration)
			assert.True(t, features.durationUnnormalized)
		})
	}
}

func TestInvalidDurationAliasesOnlyExcludeChannelsWithDurationRules(t *testing.T) {
	truncate(t)
	publicModel := "unnormalized-duration-public"
	noDurationRule := channelWithVideoModelMapping(t, 922, constant.ChannelTypeSora, publicModel, "no-duration-rule-upstream")
	mediaOnlyRule := channelWithVideoModelMapping(t, 923, constant.ChannelTypeSora, publicModel, "media-only-rule-upstream")
	durationRule := channelWithVideoModelMapping(t, 924, constant.ChannelTypeSora, publicModel, "duration-rule-upstream")
	createChannelVideoCapability(t, mediaOnlyRule, "media-only-rule-upstream", dto.VideoModelCapability{
		Images: &dto.VideoMediaRange{Max: common.GetPointer(1)},
	})
	createChannelVideoCapability(t, durationRule, "duration-rule-upstream", dto.VideoModelCapability{
		Durations: []int{6, 10, 15},
	})

	testCases := []struct {
		name string
		body string
	}{
		{
			name: "invalid alias",
			body: `{
				"model":"unnormalized-duration-public",
				"images":["https://example.com/reference.png"],
				"duration":10,
				"seconds":"10.5"
			}`,
		},
		{
			name: "conflicting aliases",
			body: `{
				"model":"unnormalized-duration-public",
				"images":["https://example.com/reference.png"],
				"duration":10,
				"seconds":"15"
			}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", testCase.body)

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.True(t, features.durationUnnormalized)
			assert.True(t, ChannelSupportsRequestConstraints(c, noDurationRule, publicModel))
			assert.True(t, ChannelSupportsRequestConstraints(c, mediaOnlyRule, publicModel))
			assert.False(t, ChannelSupportsRequestConstraints(c, durationRule, publicModel))

			evaluation := EvaluateChannelVideoRouting(durationRule, publicModel, features)
			assert.Contains(t, evaluation.Violations, VideoConstraintViolation{
				Code: "duration_unparseable", Field: "duration",
			})
		})
	}
}

func TestMetadataDurationConstrainsChannelRouting(t *testing.T) {
	truncate(t)
	publicModel := "metadata-duration-public"
	upstreamModel := "metadata-duration-upstream"
	channel := channelWithVideoModelMapping(t, 921, constant.ChannelTypeSora, publicModel, upstreamModel)
	createChannelVideoCapability(t, channel, upstreamModel, dto.VideoModelCapability{
		Durations: []int{6, 10, 15},
	})

	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"metadata-duration-public",
		"metadata":{"duration":"20s"}
	}`)

	assert.False(t, ChannelSupportsRequestConstraints(c, channel, publicModel))
}

func TestGetVideoRequestFeaturesNormalizesResolution(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"sd-bak-1",
		"resolution":"1080P",
		"metadata":{"resolution":"720p"}
	}`)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, "1080p", features.Resolution)
}

func TestGetVideoRequestFeaturesNormalizesOutputAliasesAndPixelSize(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"video-model",
		"ratio":"32:18",
		"size":"960X540"
	}`)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, "16:9", features.AspectRatio)
	assert.Equal(t, "960x540", features.Size)
	assert.Empty(t, features.Resolution)
}

func TestGetVideoRequestFeaturesAcceptsExplicitResolutionWithPixelSize(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"video-model",
		"size":"960x540",
		"resolution":"720p"
	}`)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, "960x540", features.Size)
	assert.Equal(t, "720p", features.Resolution)
}

func TestNestedProviderResolutionHintsConstrainOnlyOwningChannels(t *testing.T) {
	truncate(t)
	publicModel := "provider-resolution-hints-public"
	capability := dto.VideoModelCapability{Resolutions: []string{"720p"}}
	sora := channelWithVideoModelMapping(t, 904, constant.ChannelTypeSora, publicModel, "sora-upstream")
	openAI := channelWithVideoModelMapping(t, 908, constant.ChannelTypeOpenAI, publicModel, "openai-sora-upstream")
	gemini := channelWithVideoModelMapping(t, 905, constant.ChannelTypeGemini, publicModel, "gemini-upstream")
	vertex := channelWithVideoModelMapping(t, 906, constant.ChannelTypeVertexAi, publicModel, "vertex-upstream")
	aggc := channelWithVideoModelMapping(t, 907, constant.ChannelTypeAGGC, publicModel, "aggc-upstream")
	createChannelVideoCapability(t, sora, "sora-upstream", capability)
	createChannelVideoCapability(t, openAI, "openai-sora-upstream", capability)
	createChannelVideoCapability(t, gemini, "gemini-upstream", capability)
	createChannelVideoCapability(t, vertex, "vertex-upstream", capability)
	createChannelVideoCapability(t, aggc, "aggc-upstream", capability)

	c := newChannelConstraintTestContext(t, "/v1/videos", `{
		"model":"provider-resolution-hints-public",
		"prompt":"test",
		"parameters":{"resolution":"1080P"},
		"params":{"resolution":"2160P"}
	}`)
	features, err := GetVideoRequestFeatures(c)
	require.NoError(t, err)
	assert.Empty(t, features.Resolution)

	assert.False(t, ChannelSupportsRequestConstraints(c, sora, publicModel))
	assert.False(t, ChannelSupportsRequestConstraints(c, openAI, publicModel))
	assert.True(t, ChannelSupportsRequestConstraints(c, gemini, publicModel))
	assert.True(t, ChannelSupportsRequestConstraints(c, vertex, publicModel))
	assert.False(t, ChannelSupportsRequestConstraints(c, aggc, publicModel))

	filter, err := channelFilterForRequest(c, publicModel)
	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.False(t, filter(sora))
	assert.False(t, filter(openAI))
	assert.True(t, filter(gemini))
	assert.True(t, filter(vertex))
	assert.False(t, filter(aggc))

	for _, testCase := range []struct {
		name string
		body string
	}{
		{
			name: "top level resolution wins",
			body: `{
				"model":"provider-resolution-hints-public",
				"prompt":"test",
				"resolution":"720P",
				"parameters":{"resolution":"1080P"},
				"params":{"resolution":"2160P"}
			}`,
		},
		{
			name: "metadata resolution remains standard",
			body: `{
				"model":"provider-resolution-hints-public",
				"prompt":"test",
				"metadata":{"resolution":"720P"},
				"parameters":{"resolution":"1080P"},
				"params":{"resolution":"2160P"}
			}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", testCase.body)
			features, err := GetVideoRequestFeatures(c)
			require.NoError(t, err)
			assert.Equal(t, "720p", features.Resolution)

			assert.True(t, ChannelSupportsRequestConstraints(c, sora, publicModel))
			assert.True(t, ChannelSupportsRequestConstraints(c, openAI, publicModel))
			assert.True(t, ChannelSupportsRequestConstraints(c, gemini, publicModel))
			assert.True(t, ChannelSupportsRequestConstraints(c, vertex, publicModel))
			assert.True(t, ChannelSupportsRequestConstraints(c, aggc, publicModel))
		})
	}
}

func TestNormalizeVideoRoutingSimulationRequestUsesPublicOutputContract(t *testing.T) {
	request := VideoRoutingSimulationRequest{
		Ratio:            "32:18",
		AspectRatioAlias: "16:9",
		Size:             "960X540",
		Resolution:       "2160P",
	}

	err := NormalizeVideoRoutingSimulationRequest(&request)

	require.NoError(t, err)
	assert.Empty(t, request.Ratio)
	assert.Empty(t, request.AspectRatioAlias)
	assert.Equal(t, "16:9", request.AspectRatio)
	assert.Equal(t, "960x540", request.Size)
	assert.Equal(t, "4k", request.Resolution)
}

func TestSimulateVideoRoutingDeclaresPublicFieldConstraintScope(t *testing.T) {
	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{})

	require.NoError(t, err)
	assert.Equal(t, videoRoutingSimulationConstraintScope, result.ConstraintScope)
}

func TestNormalizeVideoRoutingSimulationRequestRejectsConflictingOutputFields(t *testing.T) {
	request := VideoRoutingSimulationRequest{
		AspectRatio: "9:16",
		Size:        "960x540",
	}

	err := NormalizeVideoRoutingSimulationRequest(&request)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts with aspect_ratio")
}

func TestMatchVideoCapabilityRejectsUnsupportedOutputConstraints(t *testing.T) {
	capability := dto.VideoModelCapability{
		AspectRatios: []string{"16:9"},
		Sizes:        []string{"1280x720"},
		Resolutions:  []string{"720p"},
	}

	violations := MatchVideoCapability(VideoRequestFeatures{
		AspectRatio: "9:16",
		Size:        "960x540",
		Resolution:  "1080p",
	}, capability)

	assert.Contains(t, violations, VideoConstraintViolation{
		Code:                  "aspect_ratio_not_supported",
		Field:                 "aspect_ratio",
		AspectRatio:           "9:16",
		SupportedAspectRatios: []string{"16:9"},
	})
	assert.Contains(t, violations, VideoConstraintViolation{
		Code:           "size_not_supported",
		Field:          "size",
		Size:           "960x540",
		SupportedSizes: []string{"1280x720"},
	})
	assert.Contains(t, violations, VideoConstraintViolation{
		Code:                 "resolution_not_supported",
		Field:                "resolution",
		Resolution:           "1080p",
		SupportedResolutions: []string{"720p"},
	})
}

func TestChannelVideoCapabilityAcceptsOutputOnlyConstraints(t *testing.T) {
	truncate(t)
	publicModel := "output-only-public"
	upstreamModel := "output-only-upstream"
	channel := channelWithVideoModelMapping(t, 90, constant.ChannelTypeYobox, publicModel, upstreamModel)
	createChannelVideoCapability(t, channel, upstreamModel, dto.VideoModelCapability{
		AspectRatios: []string{"16:9"},
		Sizes:        []string{"1280x720"},
	})

	eligible := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{
		AspectRatio: "16:9",
		Size:        "1280x720",
	})
	unsupported := EvaluateChannelVideoRouting(channel, publicModel, VideoRequestFeatures{
		AspectRatio: "9:16",
		Size:        "720x1280",
	})

	assert.True(t, eligible.Eligible)
	assert.False(t, unsupported.Eligible)
	assert.Contains(t, unsupported.Violations, VideoConstraintViolation{
		Code: "aspect_ratio_not_supported", Field: "aspect_ratio", AspectRatio: "9:16",
		SupportedAspectRatios: []string{"16:9"},
	})
	assert.Contains(t, unsupported.Violations, VideoConstraintViolation{
		Code: "size_not_supported", Field: "size", Size: "720x1280",
		SupportedSizes: []string{"1280x720"},
	})
}

func TestGetVideoRequestFeaturesReturnsTypedErrorForInvalidMediaField(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"video-model","images":{"url":"x"}}`)

	_, err := GetVideoRequestFeatures(c)

	var featuresErr *VideoRequestFeaturesError
	require.ErrorAs(t, err, &featuresErr)
	assert.Contains(t, featuresErr.Error(), "task media field")
}

func TestGetVideoRequestFeaturesIgnoresBooleanAudioSwitch(t *testing.T) {
	for _, audio := range []string{"true", "false"} {
		t.Run(audio, func(t *testing.T) {
			c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"video-model","prompt":"animate","audio":`+audio+`}`)

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

func TestGetVideoRequestFeaturesReadsMetadataOutputFromFormBodies(t *testing.T) {
	testCases := []struct {
		name    string
		request func(t *testing.T) *http.Request
	}{
		{
			name: "multipart",
			request: func(t *testing.T) *http.Request {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("metadata", `{"size":"720x1280","aspectRatio":"9:16","resolution":"1080P","duration":"12s"}`))
				require.NoError(t, writer.Close())
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
				request.Header.Set("Content-Type", writer.FormDataContentType())
				return request
			},
		},
		{
			name: "url encoded",
			request: func(t *testing.T) *http.Request {
				values := url.Values{}
				values.Set("metadata", `{"size":"720x1280","aspectRatio":"9:16","resolution":"1080P","duration":"12s"}`)
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(values.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return request
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = testCase.request(t)
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.Equal(t, "9:16", features.AspectRatio)
			assert.Equal(t, "720x1280", features.Size)
			assert.Equal(t, "1080p", features.Resolution)
			assert.Equal(t, common.GetPointer(12), features.Duration)
		})
	}
}

func TestGetVideoRequestFeaturesMarksConflictingFormDurationAliasesForRouting(t *testing.T) {
	testCases := []struct {
		name    string
		request func(t *testing.T) *http.Request
	}{
		{
			name: "multipart",
			request: func(t *testing.T) *http.Request {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("seconds", "20"))
				require.NoError(t, writer.WriteField("metadata", `{"duration":"12s"}`))
				require.NoError(t, writer.Close())
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
				request.Header.Set("Content-Type", writer.FormDataContentType())
				return request
			},
		},
		{
			name: "url encoded",
			request: func(t *testing.T) *http.Request {
				values := url.Values{}
				values.Set("seconds", "20")
				values.Set("metadata", `{"duration":"12s"}`)
				request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(values.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return request
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = testCase.request(t)
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			features, err := GetVideoRequestFeatures(c)

			require.NoError(t, err)
			assert.Equal(t, common.GetPointer(20), features.Duration)
			assert.True(t, features.durationUnnormalized)
		})
	}
}

func TestMultipartMetadataOutputConstrainsChannelRouting(t *testing.T) {
	truncate(t)
	publicModel := "multipart-metadata-output-public"
	upstreamModel := "multipart-metadata-output-upstream"
	channel := channelWithVideoModelMapping(t, 911, constant.ChannelTypeYobox, publicModel, upstreamModel)
	createChannelVideoCapability(t, channel, upstreamModel, dto.VideoModelCapability{
		AspectRatios: []string{"16:9"},
		Resolutions:  []string{"720p"},
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("metadata", `{"aspect_ratio":"9:16","resolution":"1080p"}`))
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	assert.False(t, ChannelSupportsRequestConstraints(c, channel, publicModel))
}

func TestGetVideoRequestFeaturesCountsJimengDimensioNumberedMultipartFiles(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range []string{"image_file_1", "image_file_2", "video_file_1", "audio_file_1", "audio_file_2"} {
		fileWriter, err := writer.CreateFormFile(field, field+".bin")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte(field))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, 2, features.Images)
	assert.Equal(t, 1, features.Videos)
	assert.Equal(t, 2, features.Audios)
}

func TestChannelSupportsRequestConstraintsRejectsDimensioNumberedFilesBeyondExactCapability(t *testing.T) {
	truncate(t)
	publicModel := "dimensio-numbered-files-public"
	upstreamModel := "dimensio-numbered-files-upstream"
	channel := channelWithVideoModelMapping(t, 909, constant.ChannelTypeJimengDimensio, publicModel, upstreamModel)
	createChannelVideoCapability(t, channel, upstreamModel, dto.VideoModelCapability{
		Images:          &dto.VideoMediaRange{Max: common.GetPointer(1)},
		Videos:          &dto.VideoMediaRange{Max: common.GetPointer(0)},
		Audios:          &dto.VideoMediaRange{Max: common.GetPointer(1)},
		VideoAudioTotal: &dto.VideoMediaRange{Max: common.GetPointer(1)},
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", publicModel))
	for _, field := range []string{"image_file_1", "image_file_2", "video_file_1", "audio_file_1", "audio_file_2"} {
		fileWriter, err := writer.CreateFormFile(field, field+".bin")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte(field))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	assert.False(t, ChannelSupportsRequestConstraints(c, channel, publicModel))
}

func TestGetVideoRequestFeaturesIgnoresNonVideoRoute(t *testing.T) {
	c := newChannelConstraintTestContext(t, "/v1/images/generations", `{"images":["1"]}`)

	features, err := GetVideoRequestFeatures(c)

	require.NoError(t, err)
	assert.Equal(t, VideoRequestFeatures{}, features)
}

func TestChannelSupportsRequestConstraintsUsesExactCapability(t *testing.T) {
	truncate(t)
	publicModel := "constraint-public"
	channel := channelWithVideoModelMapping(t, 84, constant.ChannelTypeYobox, publicModel, "constraint-upstream")
	createChannelVideoCapability(t, channel, "constraint-upstream", dto.VideoModelCapability{
		Videos: &dto.VideoMediaRange{Max: common.GetPointer(0)},
	})
	c := newChannelConstraintTestContext(t, "/v1/videos", `{"model":"constraint-public","videos":["v1"]}`)

	assert.False(t, ChannelSupportsRequestConstraints(c, channel, publicModel))
}

func TestSimulateVideoRoutingUsesExactCapabilityByMappedModel(t *testing.T) {
	truncate(t)
	group := "creative-video"
	publicModel := "seedance-public"
	noFacePriority := int64(20)
	standardPriority := int64(10)
	weight := uint(100)
	noFace := channelWithVideoModelMapping(t, 85, constant.ChannelTypeYobox, publicModel, "seedance-2.0-fast-noface")
	noFace.Priority = &noFacePriority
	noFace.Weight = &weight
	standard := channelWithVideoModelMapping(t, 86, constant.ChannelTypeYobox, publicModel, "seedance-2.0")
	standard.Priority = &standardPriority
	standard.Weight = &weight
	createChannelVideoCapability(t, noFace, "seedance-2.0-fast-noface", dto.VideoModelCapability{
		VideoAudioTotal: &dto.VideoMediaRange{Max: common.GetPointer(3)},
	})
	require.NoError(t, model.DB.Create(standard).Error)
	require.NoError(t, model.DB.Create(&[]model.Ability{
		{Group: group, Model: publicModel, ChannelId: noFace.Id, Enabled: true, Priority: &noFacePriority, Weight: weight},
		{Group: group, Model: publicModel, ChannelId: standard.Id, Enabled: true, Priority: &standardPriority, Weight: weight},
	}).Error)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: publicModel, Group: group, Videos: 2, Audios: 2, ContentType: "application/json",
	})

	require.NoError(t, err)
	require.NotNil(t, result.TargetPriority)
	assert.Equal(t, standardPriority, *result.TargetPriority)
	for _, candidate := range result.Candidates {
		if candidate.ChannelID == noFace.Id {
			require.NotNil(t, candidate.Eligible)
			assert.False(t, *candidate.Eligible)
			assert.Contains(t, candidate.Violations, VideoConstraintViolation{
				Code: "video_audio_total_above_max", Field: "video_audio_total", Actual: common.GetPointer(4), Expected: common.GetPointer(3),
			})
		}
	}
}

func TestSimulateVideoRoutingSelectsChannelByDiscreteDurations(t *testing.T) {
	truncate(t)
	group := "creative-video"
	publicModel := "grok-image-video"
	mappedPriority := int64(10)
	directPriority := int64(1)
	weight := uint(100)

	mapped := channelWithVideoModelMapping(t, 184, constant.ChannelTypeSora, publicModel, "grok-video-3")
	mapped.Priority = &mappedPriority
	mapped.Weight = &weight
	direct := &model.Channel{
		Id: 185, Name: "direct-grok", Type: constant.ChannelTypeSora, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &directPriority, Weight: &weight,
	}
	createChannelVideoCapability(t, mapped, "grok-video-3", dto.VideoModelCapability{Durations: []int{6, 10, 12, 16, 20}})
	createChannelVideoCapability(t, direct, publicModel, dto.VideoModelCapability{Durations: []int{6, 10, 15}})
	require.NoError(t, model.DB.Create(&[]model.Ability{
		{Group: group, Model: publicModel, ChannelId: mapped.Id, Enabled: true, Priority: &mappedPriority, Weight: weight},
		{Group: group, Model: publicModel, ChannelId: direct.Id, Enabled: true, Priority: &directPriority, Weight: weight},
	}).Error)

	tests := []struct {
		name             string
		duration         int
		selectedPriority int64
		excludedChannel  int
		supported        []int
	}{
		{name: "twenty seconds uses mapped channel", duration: 20, selectedPriority: mappedPriority, excludedChannel: direct.Id, supported: []int{6, 10, 15}},
		{name: "fifteen seconds uses direct channel", duration: 15, selectedPriority: directPriority, excludedChannel: mapped.Id, supported: []int{6, 10, 12, 16, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
				Model: publicModel, Group: group, Duration: common.GetPointer(tt.duration), ContentType: "application/json",
			})

			require.NoError(t, err)
			require.NotNil(t, result.TargetPriority)
			assert.Equal(t, tt.selectedPriority, *result.TargetPriority)
			for _, candidate := range result.Candidates {
				if candidate.ChannelID != tt.excludedChannel {
					continue
				}
				require.NotNil(t, candidate.Eligible)
				assert.False(t, *candidate.Eligible)
				assert.Contains(t, candidate.Violations, VideoConstraintViolation{
					Code: "duration_not_supported", Field: "duration", Actual: common.GetPointer(tt.duration), SupportedDurations: tt.supported,
				})
			}
		})
	}
}

func TestSimulateVideoRoutingFiltersUnsupportedResolution(t *testing.T) {
	truncate(t)
	group := "resolution-group"
	modelName := StrictVideoRoutingModelSDBak1
	priority := int64(10)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{modelName: "resolution-upstream"})
	require.NoError(t, err)
	mappingString := string(mapping)
	channel := model.Channel{
		Id: 301, Name: "720p only", Type: constant.ChannelTypeSora, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight, ModelMapping: &mappingString,
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	_, err = UpsertChannelVideoRoutingCapabilityRule(channel.Id, "resolution-upstream", dto.VideoModelCapability{
		Resolutions: []string{"720p"},
	}, 0, 1)
	require.NoError(t, err)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: modelName, Group: group, Resolution: "1080p", ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.False(t, *result.Candidates[0].Eligible)
	assert.Nil(t, result.TargetPriority)
	assert.Contains(t, result.Candidates[0].Violations, VideoConstraintViolation{
		Code:                 "resolution_not_supported",
		Field:                "resolution",
		Resolution:           "1080p",
		SupportedResolutions: []string{"720p"},
	})
}

func TestSimulateVideoRoutingFiltersUnsupportedAspectRatioAndSize(t *testing.T) {
	truncate(t)
	group := "output-constraints-group"
	modelName := StrictVideoRoutingModelSDBak1
	priority := int64(10)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{modelName: "output-constraints-upstream"})
	require.NoError(t, err)
	mappingString := string(mapping)
	channel := model.Channel{
		Id: 302, Name: "landscape only", Type: constant.ChannelTypeSora, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight, ModelMapping: &mappingString,
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	_, err = UpsertChannelVideoRoutingCapabilityRule(channel.Id, "output-constraints-upstream", dto.VideoModelCapability{
		AspectRatios: []string{"16:9"},
		Sizes:        []string{"1280x720"},
	}, 0, 1)
	require.NoError(t, err)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: modelName, Group: group, AspectRatio: "9:16", Size: "720x1280", ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.False(t, *result.Candidates[0].Eligible)
	assert.Nil(t, result.TargetPriority)
	assert.Contains(t, result.Candidates[0].Violations, VideoConstraintViolation{
		Code: "aspect_ratio_not_supported", Field: "aspect_ratio", AspectRatio: "9:16",
		SupportedAspectRatios: []string{"16:9"},
	})
	assert.Contains(t, result.Candidates[0].Violations, VideoConstraintViolation{
		Code: "size_not_supported", Field: "size", Size: "720x1280",
		SupportedSizes: []string{"1280x720"},
	})
}

func TestSimulateVideoRoutingDerivesAspectRatioFromPixelSize(t *testing.T) {
	truncate(t)
	group := "derived-output-constraints-group"
	modelName := StrictVideoRoutingModelSDBak1
	priority := int64(10)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{modelName: "derived-output-constraints-upstream"})
	require.NoError(t, err)
	mappingString := string(mapping)
	channel := model.Channel{
		Id: 303, Name: "derived landscape", Type: constant.ChannelTypeSora, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight, ModelMapping: &mappingString,
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: modelName, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	_, err = UpsertChannelVideoRoutingCapabilityRule(channel.Id, "derived-output-constraints-upstream", dto.VideoModelCapability{
		AspectRatios: []string{"16:9"},
		Sizes:        []string{"1280x720"},
	}, 0, 1)
	require.NoError(t, err)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: modelName, Group: group, Size: "1280X720", ContentType: "application/json",
	})

	require.NoError(t, err)
	assert.Equal(t, "16:9", result.Features.AspectRatio)
	assert.Equal(t, "1280x720", result.Features.Size)
	require.Len(t, result.Candidates, 1)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.True(t, *result.Candidates[0].Eligible)
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
	publicModel := "custom-video"
	priority := int64(20)
	weight := uint(100)
	mapping, err := common.Marshal(map[string]string{publicModel: "custom-upstream"})
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
		Id: 87, Name: "Alternate video route", Type: constant.ChannelTypeAdvancedCustom, Key: "key",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		ModelMapping: &mappingString, OtherSettings: string(otherSettings),
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: group, Model: publicModel, ChannelId: channel.Id, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)

	primaryRules, err := GetVideoRoutingRuleSetForPath(publicModel, group, DefaultVideoRoutingRequestPath)
	require.NoError(t, err)
	require.Len(t, primaryRules.Candidates, 1)
	assert.Equal(t, "request_path_not_supported", primaryRules.Candidates[0].ConfigurationError)

	result, err := SimulateVideoRouting(VideoRoutingSimulationRequest{
		Model: publicModel, Group: group, Images: 1, ContentType: "application/json", RequestPath: "/v1/video/generations",
	})
	require.NoError(t, err)
	require.Len(t, result.Candidates, 1)
	require.NotNil(t, result.Candidates[0].Eligible)
	assert.True(t, *result.Candidates[0].Eligible)
	require.NotNil(t, result.TargetPriority)
	assert.Equal(t, priority, *result.TargetPriority)
}

func TestVideoRoutingWritesRejectOverlongModelNames(t *testing.T) {
	truncate(t)
	overlongModel := strings.Repeat("m", 256)

	_, err := UpsertVideoRoutingPolicy(overlongModel, true, 0, 1)
	assert.EqualError(t, err, "public model must not exceed 255 characters")

	_, err = UpsertChannelVideoRoutingCapabilityRule(1, overlongModel, dto.VideoModelCapability{
		RequireJSON: common.GetPointer(false),
	}, 0, 1)
	assert.EqualError(t, err, "upstream model must not exceed 255 characters")
}
