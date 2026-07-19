package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	VideoRoutingScopeChannelType      = "channel_type"
	VideoRoutingScopeUpstreamModel    = "upstream_model"
	VideoRoutingScopeChannelTypeModel = "channel_type_model"
	VideoRoutingScopeChannel          = "channel"
	VideoRoutingScopeChannelModel     = "channel_model"
)

var ErrVideoRoutingRevisionConflict = errors.New("video routing rule revision conflict")

// VideoRoutingRuleTablesAvailable reports whether both routing rule tables are
// present. Slave nodes intentionally skip migrations, so this check allows
// them to start while a master node is still applying the schema migration.
func VideoRoutingRuleTablesAvailable() bool {
	if DB == nil {
		return false
	}
	migrator := DB.Migrator()
	return migrator.HasTable(&VideoRoutingPolicy{}) && migrator.HasTable(&VideoRoutingCapabilityRule{})
}

// VideoRoutingPolicy stores public-model behavior that is independent of a
// specific channel candidate.
type VideoRoutingPolicy struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model" gorm:"type:varchar(255);not null;uniqueIndex:uk_video_routing_policy_model"`
	Strict      bool   `json:"strict" gorm:"not null"`
	Revision    int    `json:"revision" gorm:"not null;default:1"`
	UpdatedBy   int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedTime int64  `json:"created_time" gorm:"bigint;not null"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint;not null"`
}

// VideoRoutingCapabilityRule stores a capability override at one resolution
// scope. Capability is JSON stored as TEXT so all supported databases behave
// consistently without database-specific JSON operators.
type VideoRoutingCapabilityRule struct {
	Id            int    `json:"id"`
	Scope         string `json:"scope" gorm:"type:varchar(32);not null;uniqueIndex:uk_video_routing_capability_scope,priority:1"`
	ChannelType   int    `json:"channel_type" gorm:"not null;default:0;uniqueIndex:uk_video_routing_capability_scope,priority:2"`
	ChannelId     int    `json:"channel_id" gorm:"column:channel_id;not null;default:0;uniqueIndex:uk_video_routing_capability_scope,priority:3"`
	UpstreamModel string `json:"upstream_model" gorm:"type:varchar(255);not null;default:'';uniqueIndex:uk_video_routing_capability_scope,priority:4"`
	Capability    string `json:"-" gorm:"type:text;not null"`
	Revision      int    `json:"revision" gorm:"not null;default:1"`
	UpdatedBy     int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedTime   int64  `json:"created_time" gorm:"bigint;not null"`
	UpdatedTime   int64  `json:"updated_time" gorm:"bigint;not null"`
}

func GetAllVideoRoutingPolicies() ([]VideoRoutingPolicy, error) {
	var policies []VideoRoutingPolicy
	err := DB.Order("public_model ASC").Find(&policies).Error
	return policies, err
}

func GetVideoRoutingPolicy(publicModel string) (*VideoRoutingPolicy, error) {
	var policy VideoRoutingPolicy
	err := DB.Where("public_model = ?", publicModel).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &policy, err
}

func UpsertVideoRoutingPolicy(publicModel string, strict bool, revision int, updatedBy int) (*VideoRoutingPolicy, error) {
	var saved VideoRoutingPolicy
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing VideoRoutingPolicy
		err := tx.Where("public_model = ?", publicModel).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if revision != 0 {
				return ErrVideoRoutingRevisionConflict
			}
			now := common.GetTimestamp()
			saved = VideoRoutingPolicy{
				PublicModel: publicModel,
				Strict:      strict,
				Revision:    1,
				UpdatedBy:   updatedBy,
				CreatedTime: now,
				UpdatedTime: now,
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&saved)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVideoRoutingRevisionConflict
			}
			return nil
		}
		if err != nil {
			return err
		}
		if revision != existing.Revision {
			return ErrVideoRoutingRevisionConflict
		}
		now := common.GetTimestamp()
		result := tx.Model(&VideoRoutingPolicy{}).
			Where("id = ? AND revision = ?", existing.Id, existing.Revision).
			Updates(map[string]any{
				"strict":       strict,
				"revision":     existing.Revision + 1,
				"updated_by":   updatedBy,
				"updated_time": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVideoRoutingRevisionConflict
		}
		return tx.First(&saved, existing.Id).Error
	})
	return &saved, err
}

func GetAllVideoRoutingCapabilityRules() ([]VideoRoutingCapabilityRule, error) {
	var rules []VideoRoutingCapabilityRule
	err := DB.Order("id ASC").Find(&rules).Error
	return rules, err
}

func GetVideoRoutingCapabilityRule(scope string, channelType int, channelId int, upstreamModel string) (*VideoRoutingCapabilityRule, error) {
	var rule VideoRoutingCapabilityRule
	err := videoRoutingCapabilityRuleQuery(DB, scope, channelType, channelId, upstreamModel).First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rule, err
}

func GetVideoRoutingCapabilityRuleById(id int) (*VideoRoutingCapabilityRule, error) {
	var rule VideoRoutingCapabilityRule
	err := DB.First(&rule, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rule, err
}

func UpsertVideoRoutingCapabilityRule(rule VideoRoutingCapabilityRule, revision int) (*VideoRoutingCapabilityRule, error) {
	var saved VideoRoutingCapabilityRule
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing VideoRoutingCapabilityRule
		err := videoRoutingCapabilityRuleQuery(tx, rule.Scope, rule.ChannelType, rule.ChannelId, rule.UpstreamModel).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if revision != 0 {
				return ErrVideoRoutingRevisionConflict
			}
			now := common.GetTimestamp()
			rule.Id = 0
			rule.Revision = 1
			rule.CreatedTime = now
			rule.UpdatedTime = now
			saved = rule
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&saved)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVideoRoutingRevisionConflict
			}
			return nil
		}
		if err != nil {
			return err
		}
		if revision != existing.Revision {
			return ErrVideoRoutingRevisionConflict
		}
		now := common.GetTimestamp()
		result := tx.Model(&VideoRoutingCapabilityRule{}).
			Where("id = ? AND revision = ?", existing.Id, existing.Revision).
			Updates(map[string]any{
				"capability":   rule.Capability,
				"revision":     existing.Revision + 1,
				"updated_by":   rule.UpdatedBy,
				"updated_time": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVideoRoutingRevisionConflict
		}
		return tx.First(&saved, existing.Id).Error
	})
	return &saved, err
}

func DeleteVideoRoutingCapabilityRule(id int, revision int) error {
	result := DB.Where("id = ? AND revision = ?", id, revision).Delete(&VideoRoutingCapabilityRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrVideoRoutingRevisionConflict
	}
	return nil
}

func videoRoutingCapabilityRuleQuery(db *gorm.DB, scope string, channelType int, channelId int, upstreamModel string) *gorm.DB {
	return db.Where(
		"scope = ? AND channel_type = ? AND channel_id = ? AND upstream_model = ?",
		scope,
		channelType,
		channelId,
		upstreamModel,
	)
}
