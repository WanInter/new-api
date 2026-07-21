package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelPricingRuleTestDB(t *testing.T) {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	oldCache := modelPricingRuleCache.Load()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &ModelPricingRule{}))
	DB = db
	LOG_DB = db
	reloadModelPricingRuleCache(nil)
	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLogDB
		modelPricingRuleCache.Store(oldCache)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestModelPricingRulesResolveBySpecificityAndRefreshCache(t *testing.T) {
	setupModelPricingRuleTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 17, Username: "pricing-test"}).Error)

	userExact := ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		UsingGroup:   "creative-video",
		Ratio:        0.9,
		Enabled:      true,
	}
	userAny := ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		Ratio:        0.85,
		Enabled:      true,
	}
	groupExact := ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUserGroup,
		SubjectValue: "vip_9折",
		Model:        "seedance2.0",
		UsingGroup:   "creative-video",
		Ratio:        0.8,
		Enabled:      true,
	}
	groupAny := ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUserGroup,
		SubjectValue: "vip_9折",
		Model:        "seedance2.0",
		Ratio:        0.75,
		Enabled:      true,
	}
	for _, rule := range []*ModelPricingRule{&userExact, &userAny, &groupExact, &groupAny} {
		require.NoError(t, CreateModelPricingRule(rule))
	}

	rule, ok := ResolveModelPricingRule(17, "vip_9折", "seedance2.0", "creative-video")
	require.True(t, ok)
	assert.Equal(t, userExact.Id, rule.Id)
	assert.Equal(t, 0.9, rule.Ratio)

	rule, ok = ResolveModelPricingRule(17, "vip_9折", "seedance2.0", "creative-image")
	require.True(t, ok)
	assert.Equal(t, userAny.Id, rule.Id)
	assert.Equal(t, 0.85, rule.Ratio)

	rule, ok = ResolveModelPricingRule(99, "vip_9折", "seedance2.0", "creative-video")
	require.True(t, ok)
	assert.Equal(t, groupExact.Id, rule.Id)
	assert.Equal(t, 0.8, rule.Ratio)

	rule, ok = ResolveModelPricingRule(99, "vip_9折", "seedance2.0", "creative-image")
	require.True(t, ok)
	assert.Equal(t, groupAny.Id, rule.Id)
	assert.Equal(t, 0.75, rule.Ratio)

	_, ok = ResolveModelPricingRule(99, "default", "seedance2.0", "creative-video")
	assert.False(t, ok)

	userExact.Enabled = false
	require.NoError(t, UpdateModelPricingRule(&userExact))
	rule, ok = ResolveModelPricingRule(17, "vip_9折", "seedance2.0", "creative-video")
	require.True(t, ok)
	assert.Equal(t, userAny.Id, rule.Id)

	require.NoError(t, DeleteModelPricingRule(userAny.Id))
	rule, ok = ResolveModelPricingRule(17, "vip_9折", "seedance2.0", "creative-video")
	require.True(t, ok)
	assert.Equal(t, groupExact.Id, rule.Id)
}

func TestModelPricingRulesValidateSubjectsAndMissingRecords(t *testing.T) {
	setupModelPricingRuleTestDB(t)

	missingUser := &ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		Ratio:        0.9,
		Enabled:      true,
	}
	err := CreateModelPricingRule(missingUser)
	assert.True(t, errors.Is(err, ErrModelPricingRuleUserNotFound))

	require.NoError(t, DB.Create(&User{Id: 17, Username: "pricing-test"}).Error)
	valid := &ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		Ratio:        0.9,
		Enabled:      true,
	}
	require.NoError(t, CreateModelPricingRule(valid))

	duplicate := *valid
	duplicate.Id = 0
	err = CreateModelPricingRule(&duplicate)
	assert.True(t, errors.Is(err, ErrModelPricingRuleConflict))

	missing := *valid
	missing.Id = 999
	err = UpdateModelPricingRule(&missing)
	assert.True(t, errors.Is(err, ErrModelPricingRuleNotFound))
	err = DeleteModelPricingRule(999)
	assert.True(t, errors.Is(err, ErrModelPricingRuleNotFound))
}

func TestGetModelPricingRulesIncludesUserSubjectName(t *testing.T) {
	setupModelPricingRuleTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 17, Username: "pricing-test"}).Error)

	userRule := &ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		Ratio:        0.9,
		Enabled:      true,
	}
	groupRule := &ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUserGroup,
		SubjectValue: "vip_9折",
		Model:        "seedance2.0-fast",
		Ratio:        0.9,
		Enabled:      true,
	}
	require.NoError(t, CreateModelPricingRule(userRule))
	require.NoError(t, CreateModelPricingRule(groupRule))

	rules, err := GetModelPricingRules()
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "pricing-test", rules[0].SubjectName)
	assert.Empty(t, rules[1].SubjectName)
}

func TestCreateModelPricingRulesCreatesAllOrNone(t *testing.T) {
	setupModelPricingRuleTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 17, Username: "pricing-test"}).Error)

	existingRule := &ModelPricingRule{
		SubjectType:  ModelPricingRuleSubjectUser,
		SubjectValue: "17",
		Model:        "seedance2.0",
		Ratio:        0.9,
		Enabled:      true,
	}
	require.NoError(t, CreateModelPricingRule(existingRule))

	createdRules := []*ModelPricingRule{
		{
			SubjectType:  ModelPricingRuleSubjectUser,
			SubjectValue: "17",
			Model:        "seedance2.0-fast",
			Ratio:        0.9,
			Enabled:      true,
		},
		{
			SubjectType:  ModelPricingRuleSubjectUser,
			SubjectValue: "17",
			Model:        "seedance2.0-S",
			Ratio:        0.9,
			Enabled:      true,
		},
	}
	require.NoError(t, CreateModelPricingRules(createdRules))
	assert.Positive(t, createdRules[0].Id)
	assert.Positive(t, createdRules[1].Id)

	conflictingRules := []*ModelPricingRule{
		{
			SubjectType:  ModelPricingRuleSubjectUser,
			SubjectValue: "17",
			Model:        "seedance2.0-fast-S",
			Ratio:        0.9,
			Enabled:      true,
		},
		{
			SubjectType:  ModelPricingRuleSubjectUser,
			SubjectValue: "17",
			Model:        "seedance2.0",
			Ratio:        0.9,
			Enabled:      true,
		},
	}
	err := CreateModelPricingRules(conflictingRules)
	assert.True(t, errors.Is(err, ErrModelPricingRuleConflict))

	var count int64
	require.NoError(t, DB.Model(&ModelPricingRule{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)
}
