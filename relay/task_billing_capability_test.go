package relay

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func taskBillingTestCapability(schema string, fields ...channel.TaskBillingField) *channel.TaskBillingCapability {
	return &channel.TaskBillingCapability{
		SchemaVersion: schema,
		Fields:        fields,
	}
}

func setupTaskBillingCapabilityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createTaskBillingCapabilityRoute(t *testing.T, db *gorm.DB, channelID, channelType int, publicModel, upstreamModel string) {
	t.Helper()
	mappingBytes, err := common.Marshal(map[string]string{publicModel: upstreamModel})
	require.NoError(t, err)
	mapping := string(mappingBytes)
	require.NoError(t, db.Create(&model.Channel{
		Id:           channelID,
		Type:         channelType,
		Name:         fmt.Sprintf("channel-%d", channelID),
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mapping,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     publicModel,
		ChannelId: channelID,
		Enabled:   true,
	}).Error)
}

func TestMergeTaskBillingCapabilitiesPreservesIdenticalSchema(t *testing.T) {
	capability := taskBillingTestCapability("video.duration.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5"},
	})

	merged, err := mergeTaskBillingCapabilities([]*channel.TaskBillingCapability{capability, capability})

	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.Equal(t, capability.SchemaVersion, merged.SchemaVersion)
	assert.Equal(t, capability.Fields, merged.Fields)
}

func TestMergeTaskBillingCapabilitiesBuildsSafeUnion(t *testing.T) {
	durationOnly := taskBillingTestCapability("video.duration.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5"},
	})
	durationAndSize := taskBillingTestCapability(
		"video.duration-size.v1",
		channel.TaskBillingField{
			Path:       "billing.duration_seconds",
			Type:       "number",
			Required:   true,
			EnumValues: []string{"5", "6"},
		},
		channel.TaskBillingField{
			Path:       "billing.size",
			Type:       "string",
			Required:   true,
			EnumValues: []string{"1280x720"},
		},
	)

	merged, err := mergeTaskBillingCapabilities([]*channel.TaskBillingCapability{durationOnly, durationAndSize})

	require.NoError(t, err)
	require.NotNil(t, merged)
	assert.True(t, strings.HasPrefix(merged.SchemaVersion, "task.canonical-merged."))
	require.Len(t, merged.Fields, 2)
	assert.Equal(t, channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5", "6"},
	}, merged.Fields[0])
	assert.Equal(t, channel.TaskBillingField{
		Path:       "billing.size",
		Type:       "string",
		Required:   false,
		EnumValues: []string{"1280x720"},
	}, merged.Fields[1])
	assert.NoError(t, taskBillingCapabilityFitsModelSchema(durationOnly, merged))
	assert.NoError(t, taskBillingCapabilityFitsModelSchema(durationAndSize, merged))
}

func TestMergeTaskBillingCapabilitiesIsOrderIndependent(t *testing.T) {
	first := taskBillingTestCapability("video.duration.a.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5"},
	})
	second := taskBillingTestCapability("video.duration.b.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"5", "6"},
	})

	left, err := mergeTaskBillingCapabilities([]*channel.TaskBillingCapability{first, second})
	require.NoError(t, err)
	right, err := mergeTaskBillingCapabilities([]*channel.TaskBillingCapability{second, first})
	require.NoError(t, err)

	assert.Equal(t, left, right)
}

func TestMergeTaskBillingCapabilitiesRejectsConflictingTypes(t *testing.T) {
	numeric := taskBillingTestCapability("video.duration.number.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"5"},
	})
	textual := taskBillingTestCapability("video.duration.string.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "string",
		Required:   true,
		EnumValues: []string{"5s"},
	})

	merged, err := mergeTaskBillingCapabilities([]*channel.TaskBillingCapability{numeric, textual})

	require.Error(t, err)
	assert.Nil(t, merged)
	assert.Contains(t, err.Error(), "conflicting types")
}

func TestTaskBillingCapabilityFitsModelSchemaRejectsUncoveredValue(t *testing.T) {
	provider := taskBillingTestCapability("video.provider.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"15"},
	})
	modelCapability := taskBillingTestCapability("video.model.v1", channel.TaskBillingField{
		Path:       "billing.duration_seconds",
		Type:       "number",
		Required:   true,
		EnumValues: []string{"4", "5"},
	})

	err := taskBillingCapabilityFitsModelSchema(provider, modelCapability)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the model schema")
}

func TestGetTaskBillingCapabilitySummaryMergesCompatibleVideoRoutes(t *testing.T) {
	db := setupTaskBillingCapabilityTestDB(t)
	const publicModel = "shared-seedance"
	createTaskBillingCapabilityRoute(t, db, 1, constant.ChannelTypeDoubaoVideo, publicModel, "bytefor-2.0-real-priority")
	createTaskBillingCapabilityRoute(t, db, 2, constant.ChannelTypeSeventhFrame, publicModel, "viraldance900--person-stripe--6c832bb1--voice-tone--a0c4ee78")

	summary, err := GetTaskBillingCapabilitySummary(publicModel)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.True(t, summary.Applicable)
	assert.True(t, summary.Compatible)
	assert.True(t, strings.HasPrefix(summary.SchemaVersion, "task.canonical-merged."))
	assert.Len(t, summary.CompatibleChannels, 2)
	assert.Empty(t, summary.IncompatibleChannels)
	require.Len(t, summary.Fields, 1)
	assert.Equal(t, "billing.duration_seconds", summary.Fields[0].Path)
	assert.True(t, summary.Fields[0].Required)
	assert.ElementsMatch(t, []string{"4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}, summary.Fields[0].EnumValues)
}

func TestGetTaskBillingCapabilitySummaryUsesChannelDefaultForUnlistedYoboxModel(t *testing.T) {
	db := setupTaskBillingCapabilityTestDB(t)
	const publicModel = "public-yobox-alias"
	createTaskBillingCapabilityRoute(t, db, 1, constant.ChannelTypeYobox, publicModel, "seedance-2.0-933")

	summary, err := GetTaskBillingCapabilitySummary(publicModel)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.True(t, summary.Applicable)
	assert.True(t, summary.Compatible)
	assert.Equal(t, "video.yobox.seedance-2.0.duration-4-15.resolution-480p-720p-1080p-4k.optional.v1", summary.SchemaVersion)
	assert.Empty(t, summary.IncompatibleChannels)
}

func TestGetTaskBillingCapabilitySummaryIgnoresGenericNonVideoRoute(t *testing.T) {
	db := setupTaskBillingCapabilityTestDB(t)
	const publicModel = "chat-model"
	createTaskBillingCapabilityRoute(t, db, 1, constant.ChannelTypeOpenAI, publicModel, "chat-model")

	summary, err := GetTaskBillingCapabilitySummary(publicModel)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.False(t, summary.Applicable)
	assert.False(t, summary.Compatible)
	assert.Empty(t, summary.CompatibleChannels)
	assert.Empty(t, summary.IncompatibleChannels)
	assert.Equal(t, "该模型不是视频任务模型", summary.Reason)
}

func TestGetTaskBillingCapabilitySummaryKeepsUnsupportedVideoRouteVisible(t *testing.T) {
	db := setupTaskBillingCapabilityTestDB(t)
	const publicModel = "public-unknown-video-model"
	createTaskBillingCapabilityRoute(t, db, 1, constant.ChannelTypeShishi, publicModel, "unpublished-video-model")

	summary, err := GetTaskBillingCapabilitySummary(publicModel)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.True(t, summary.Applicable)
	assert.False(t, summary.Compatible)
	require.Len(t, summary.IncompatibleChannels, 1)
	assert.Contains(t, summary.IncompatibleChannels[0].Incompatibility, "未声明规范计费 schema")
}
