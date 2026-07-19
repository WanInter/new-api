package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupImageRoutingTestDB(t *testing.T) {
	t.Helper()
	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.ImageRoutingPolicy{},
		&model.ImageRoutingSize{},
		&model.ImageRoutingRule{},
	))
	model.DB = db
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	require.NoError(t, ReloadImageRoutingRuleCache())
	t.Cleanup(func() {
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		if oldDB != nil && oldMemoryCacheEnabled {
			model.InitChannelCache()
		}
		imageRoutingRules.Store(newImageRoutingRuleSnapshot())
	})
}

func createImageRoutingTestChannel(t *testing.T, id int, name string, status int, group string, publicModel string) {
	t.Helper()
	priority := int64(0)
	channel := model.Channel{
		Id:       id,
		Name:     name,
		Status:   status,
		Type:     1,
		Group:    group,
		Models:   publicModel,
		Priority: &priority,
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     group,
		Model:     publicModel,
		ChannelId: id,
		Enabled:   status == common.ChannelStatusEnabled,
		Priority:  &priority,
		Weight:    100,
	}).Error)
	model.InitChannelCache()
}

func newImageRoutingTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestNormalizeImageRoutingSize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "1024x1024", expected: "1024x1024"},
		{input: " 2048 X 2048 ", expected: "2048x2048"},
		{input: "3840×2160", expected: "3840x2160"},
	}
	for _, test := range tests {
		actual, err := NormalizeImageRoutingSize(test.input)
		require.NoError(t, err)
		assert.Equal(t, test.expected, actual)
	}
	_, err := NormalizeImageRoutingSize("square")
	assert.Error(t, err)
}

func TestReplaceImageRoutingConfigPersistsAtomicCatalogAndRevision(t *testing.T) {
	setupImageRoutingTestDB(t)
	createImageRoutingTestChannel(t, 43, "NIO", common.ChannelStatusEnabled, "default", "image2")

	config, err := ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      true,
		DefaultSize: "1024×1024",
		Sizes: []ImageRoutingSizeInput{
			{Size: "1024x1024", Tier: "1K", Sort: 1},
		},
		Rules: []ImageRoutingRuleInput{
			{Tier: "1K", ChannelID: 43, Rank: 1},
		},
	}, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, config.Revision)
	assert.Equal(t, "1024x1024", config.DefaultSize)
	assert.Equal(t, ImageRoutingTier1K, config.Sizes[0].Tier)

	_, err = ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      true,
		DefaultSize: "1024x1024",
		Revision:    0,
		Sizes:       config.Sizes,
		Rules:       config.Rules,
	}, 2)
	assert.ErrorIs(t, err, model.ErrImageRoutingRevisionConflict)
}

func TestImageRoutingUsesDefaultSizeAndSkipsUnavailableFirstRule(t *testing.T) {
	setupImageRoutingTestDB(t)
	createImageRoutingTestChannel(t, 43, "NIO disabled", common.ChannelStatusManuallyDisabled, "default", "image2")
	createImageRoutingTestChannel(t, 51, "Vibe", common.ChannelStatusEnabled, "default", "image2")
	createImageRoutingTestChannel(t, 62, "YS", common.ChannelStatusEnabled, "default", "image2")
	_, err := ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      true,
		DefaultSize: "1024x1024",
		Sizes: []ImageRoutingSizeInput{
			{Size: "1024x1024", Tier: "1k", Sort: 1},
		},
		Rules: []ImageRoutingRuleInput{
			{Tier: "1k", ChannelID: 43, Rank: 1},
			{Tier: "1k", ChannelID: 51, Rank: 2},
			{Tier: "1k", ChannelID: 62, Rank: 3},
		},
	}, 1)
	require.NoError(t, err)

	c := newImageRoutingTestContext(t, `{"model":"image2","prompt":"cat"}`)
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         c,
		TokenGroup:  "default",
		ModelName:   "image2",
		RequestPath: "/v1/images/generations",
		Retry:       common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, "default", group)
	assert.Equal(t, 51, channel.Id)

	c.Set("use_channel", []string{"51"})
	channel, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         c,
		TokenGroup:  "default",
		ModelName:   "image2",
		RequestPath: "/v1/images/generations",
		Retry:       common.GetPointer(1),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 62, channel.Id)
}

func TestStrictImageRoutingRejectsUnknownSize(t *testing.T) {
	setupImageRoutingTestDB(t)
	createImageRoutingTestChannel(t, 43, "NIO", common.ChannelStatusEnabled, "default", "image2")
	_, err := ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      true,
		DefaultSize: "1024x1024",
		Sizes:       []ImageRoutingSizeInput{{Size: "1024x1024", Tier: "1k", Sort: 1}},
		Rules:       []ImageRoutingRuleInput{{Tier: "1k", ChannelID: 43, Rank: 1}},
	}, 1)
	require.NoError(t, err)

	c := newImageRoutingTestContext(t, `{"model":"image2","prompt":"cat","size":"1234x567"}`)
	_, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         c,
		TokenGroup:  "default",
		ModelName:   "image2",
		RequestPath: "/v1/images/generations",
		Retry:       common.GetPointer(0),
	})
	assert.True(t, IsImageRoutingRequestError(err))
	assert.Contains(t, err.Error(), "not configured")
}

func TestNonStrictImageRoutingUnknownSizeKeepsChannelAffinity(t *testing.T) {
	setupImageRoutingTestDB(t)
	createImageRoutingTestChannel(t, 43, "NIO", common.ChannelStatusEnabled, "default", "image2")
	_, err := ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      false,
		DefaultSize: "1024x1024",
		Sizes:       []ImageRoutingSizeInput{{Size: "1024x1024", Tier: "1k", Sort: 1}},
		Rules:       []ImageRoutingRuleInput{{Tier: "1k", ChannelID: 43, Rank: 1}},
	}, 1)
	require.NoError(t, err)

	c := newImageRoutingTestContext(t, `{"model":"image2","prompt":"cat","size":"1234x567"}`)
	assert.False(t, ShouldBypassImageRoutingAffinity(c, "image2"))
}

func TestStrictImageRoutingChecksAllAutoGroupsBeforeUnavailableError(t *testing.T) {
	setupImageRoutingTestDB(t)
	oldAutoGroups := setting.AutoGroups2JsonString()
	oldUsableGroups := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["group-a","group-b"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"group-a":"A","group-b":"B"}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsableGroups))
	})
	createImageRoutingTestChannel(t, 51, "Vibe", common.ChannelStatusEnabled, "group-b", "image2")
	_, err := ReplaceImageRoutingConfig(ReplaceImageRoutingConfigRequest{
		PublicModel: "image2",
		Strict:      true,
		DefaultSize: "1024x1024",
		Sizes:       []ImageRoutingSizeInput{{Size: "1024x1024", Tier: "1k", Sort: 1}},
		Rules:       []ImageRoutingRuleInput{{Tier: "1k", ChannelID: 51, Rank: 1}},
	}, 1)
	require.NoError(t, err)

	c := newImageRoutingTestContext(t, `{"model":"image2","prompt":"cat"}`)
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         c,
		TokenGroup:  "auto",
		ModelName:   "image2",
		RequestPath: "/v1/images/generations",
		Retry:       common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 51, channel.Id)
	assert.Equal(t, "group-b", group)
}

func TestUnconfiguredImageModelKeepsStaticSelection(t *testing.T) {
	setupImageRoutingTestDB(t)
	createImageRoutingTestChannel(t, 70, "Static", common.ChannelStatusEnabled, "default", "other-image")
	c := newImageRoutingTestContext(t, `{"model":"other-image","prompt":"cat","size":"1234x567"}`)

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:         c,
		TokenGroup:  "default",
		ModelName:   "other-image",
		RequestPath: "/v1/images/generations",
		Retry:       common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 70, channel.Id)
}
