package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoRoutingModelTestDB(t *testing.T) {
	t.Helper()
	oldDB := DB
	oldLogDB := LOG_DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Channel{},
		&Ability{},
		&VideoRoutingPolicy{},
		&VideoRoutingCapabilityRule{},
	))
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestUpsertVideoRoutingPolicyPreservesFalseAndRejectsStaleRevision(t *testing.T) {
	setupVideoRoutingModelTestDB(t)

	created, err := UpsertVideoRoutingPolicy("sd-bak-1", false, 0, 7)
	require.NoError(t, err)
	assert.False(t, created.Strict)
	assert.Equal(t, 1, created.Revision)

	updated, err := UpsertVideoRoutingPolicy("sd-bak-1", true, created.Revision, 8)
	require.NoError(t, err)
	assert.True(t, updated.Strict)
	assert.Equal(t, 2, updated.Revision)
	assert.Equal(t, 8, updated.UpdatedBy)

	_, err = UpsertVideoRoutingPolicy("sd-bak-1", false, created.Revision, 9)
	assert.True(t, errors.Is(err, ErrVideoRoutingRevisionConflict))
}

func TestVideoRoutingCapabilityRuleRevisionProtectsUpdateAndDelete(t *testing.T) {
	setupVideoRoutingModelTestDB(t)

	created, err := UpsertVideoRoutingCapabilityRule(VideoRoutingCapabilityRule{
		Scope:         VideoRoutingScopeChannelModel,
		ChannelId:     42,
		UpstreamModel: "seedance-2.0-fast",
		Capability:    `{"images":{"max":0},"require_json":false}`,
		UpdatedBy:     7,
	}, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, created.Revision)

	updated, err := UpsertVideoRoutingCapabilityRule(VideoRoutingCapabilityRule{
		Scope:         VideoRoutingScopeChannelModel,
		ChannelId:     42,
		UpstreamModel: "seedance-2.0-fast",
		Capability:    `{"images":{"max":4}}`,
		UpdatedBy:     8,
	}, created.Revision)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Revision)
	assert.Equal(t, `{"images":{"max":4}}`, updated.Capability)

	err = DeleteVideoRoutingCapabilityRule(updated.Id, created.Revision)
	assert.True(t, errors.Is(err, ErrVideoRoutingRevisionConflict))
	require.NoError(t, DeleteVideoRoutingCapabilityRule(updated.Id, updated.Revision))
}

func TestUpdatePriorityAndWeightSynchronizesAbilitiesAndPreservesZero(t *testing.T) {
	setupVideoRoutingModelTestDB(t)

	priority := int64(12)
	weight := uint(20)
	channel := Channel{
		Id:       42,
		Name:     "routing channel",
		Key:      "key",
		Status:   common.ChannelStatusEnabled,
		Group:    "creative-video",
		Models:   "seedance2.0",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	zeroPriority := int64(0)
	zeroWeight := uint(0)
	updated, err := UpdatePriorityAndWeight(channel.Id, &zeroPriority, &zeroWeight)
	require.NoError(t, err)
	require.NotNil(t, updated.Priority)
	assert.Equal(t, int64(0), *updated.Priority)
	require.NotNil(t, updated.Weight)
	assert.Equal(t, uint(0), *updated.Weight)

	var ability Ability
	require.NoError(t, DB.First(&ability, "channel_id = ?", channel.Id).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(0), *ability.Priority)
	assert.Equal(t, uint(0), ability.Weight)
}
