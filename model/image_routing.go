package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrImageRoutingRevisionConflict = errors.New("image routing rule revision conflict")

type ImageRoutingPolicy struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model" gorm:"type:varchar(255);not null;uniqueIndex:uk_image_routing_policy_model"`
	Strict      bool   `json:"strict" gorm:"not null"`
	DefaultSize string `json:"default_size" gorm:"type:varchar(32);not null"`
	Revision    int    `json:"revision" gorm:"not null;default:1"`
	UpdatedBy   int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedTime int64  `json:"created_time" gorm:"bigint;not null"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint;not null"`
}

type ImageRoutingSize struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model" gorm:"type:varchar(255);not null;uniqueIndex:uk_image_routing_size,priority:1;index:idx_image_routing_size_model"`
	Size        string `json:"size" gorm:"type:varchar(32);not null;uniqueIndex:uk_image_routing_size,priority:2"`
	Tier        string `json:"tier" gorm:"type:varchar(8);not null"`
	Sort        int    `json:"sort" gorm:"not null;default:0"`
}

type ImageRoutingRule struct {
	Id          int    `json:"id"`
	PublicModel string `json:"public_model" gorm:"type:varchar(255);not null;uniqueIndex:uk_image_routing_rule,priority:1;index:idx_image_routing_rule_model"`
	Tier        string `json:"tier" gorm:"type:varchar(8);not null;uniqueIndex:uk_image_routing_rule,priority:2"`
	ChannelId   int    `json:"channel_id" gorm:"column:channel_id;not null;uniqueIndex:uk_image_routing_rule,priority:3"`
	Rank        int    `json:"rank" gorm:"not null"`
}

func ImageRoutingRuleTablesAvailable() bool {
	if DB == nil {
		return false
	}
	migrator := DB.Migrator()
	return migrator.HasTable(&ImageRoutingPolicy{}) &&
		migrator.HasTable(&ImageRoutingSize{}) &&
		migrator.HasTable(&ImageRoutingRule{})
}

func GetAllImageRoutingPolicies() ([]ImageRoutingPolicy, error) {
	var policies []ImageRoutingPolicy
	err := DB.Order("public_model ASC").Find(&policies).Error
	return policies, err
}

func GetAllImageRoutingSizes() ([]ImageRoutingSize, error) {
	var sizes []ImageRoutingSize
	err := DB.Order("public_model ASC, sort ASC, id ASC").Find(&sizes).Error
	return sizes, err
}

func GetAllImageRoutingRules() ([]ImageRoutingRule, error) {
	var rules []ImageRoutingRule
	err := DB.Order("public_model ASC, tier ASC, rank ASC, id ASC").Find(&rules).Error
	return rules, err
}

func GetImageRoutingConfig(publicModel string) (*ImageRoutingPolicy, []ImageRoutingSize, []ImageRoutingRule, error) {
	var policy ImageRoutingPolicy
	err := DB.Where("public_model = ?", publicModel).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, []ImageRoutingSize{}, []ImageRoutingRule{}, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var sizes []ImageRoutingSize
	if err := DB.Where("public_model = ?", publicModel).Order("sort ASC, id ASC").Find(&sizes).Error; err != nil {
		return nil, nil, nil, err
	}
	var rules []ImageRoutingRule
	if err := DB.Where("public_model = ?", publicModel).Order("tier ASC, rank ASC, id ASC").Find(&rules).Error; err != nil {
		return nil, nil, nil, err
	}
	return &policy, sizes, rules, nil
}

func ReplaceImageRoutingConfig(
	publicModel string,
	strict bool,
	defaultSize string,
	revision int,
	updatedBy int,
	sizes []ImageRoutingSize,
	rules []ImageRoutingRule,
) (*ImageRoutingPolicy, error) {
	var saved ImageRoutingPolicy
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing ImageRoutingPolicy
		err := tx.Where("public_model = ?", publicModel).First(&existing).Error
		now := common.GetTimestamp()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if revision != 0 {
				return ErrImageRoutingRevisionConflict
			}
			saved = ImageRoutingPolicy{
				PublicModel: publicModel,
				Strict:      strict,
				DefaultSize: defaultSize,
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
				return ErrImageRoutingRevisionConflict
			}
		} else if err != nil {
			return err
		} else {
			if revision != existing.Revision {
				return ErrImageRoutingRevisionConflict
			}
			result := tx.Model(&ImageRoutingPolicy{}).
				Where("id = ? AND revision = ?", existing.Id, existing.Revision).
				Updates(map[string]any{
					"strict":       strict,
					"default_size": defaultSize,
					"revision":     existing.Revision + 1,
					"updated_by":   updatedBy,
					"updated_time": now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrImageRoutingRevisionConflict
			}
			if err := tx.First(&saved, existing.Id).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("public_model = ?", publicModel).Delete(&ImageRoutingSize{}).Error; err != nil {
			return err
		}
		if err := tx.Where("public_model = ?", publicModel).Delete(&ImageRoutingRule{}).Error; err != nil {
			return err
		}
		for i := range sizes {
			sizes[i].Id = 0
			sizes[i].PublicModel = publicModel
		}
		if len(sizes) > 0 {
			if err := tx.Create(&sizes).Error; err != nil {
				return err
			}
		}
		for i := range rules {
			rules[i].Id = 0
			rules[i].PublicModel = publicModel
		}
		if len(rules) > 0 {
			if err := tx.Create(&rules).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return &saved, err
}
