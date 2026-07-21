package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

const (
	ModelPricingRuleSubjectUser      = "user"
	ModelPricingRuleSubjectUserGroup = "user_group"
)

var (
	ErrModelPricingRuleNotFound          = errors.New("model pricing rule not found")
	ErrModelPricingRuleConflict          = errors.New("model pricing rule already exists for this subject, model, and using group")
	ErrModelPricingRuleUserNotFound      = errors.New("model pricing rule user does not exist")
	ErrModelPricingRuleTablesUnavailable = errors.New("model pricing rule table is unavailable")
)

// ModelPricingRule overrides the billing multiplier without changing model
// availability or channel routing. An empty UsingGroup applies to every route.
type ModelPricingRule struct {
	Id           int     `json:"id"`
	SubjectType  string  `json:"subject_type" gorm:"type:varchar(16);not null;uniqueIndex:idx_model_pricing_rule_scope,priority:1"`
	SubjectValue string  `json:"subject_value" gorm:"type:varchar(64);not null;uniqueIndex:idx_model_pricing_rule_scope,priority:2"`
	SubjectName  string  `json:"subject_name,omitempty" gorm:"-"`
	Model        string  `json:"model" gorm:"type:varchar(255);not null;uniqueIndex:idx_model_pricing_rule_scope,priority:3"`
	UsingGroup   string  `json:"using_group" gorm:"type:varchar(64);not null;default:'';uniqueIndex:idx_model_pricing_rule_scope,priority:4"`
	Ratio        float64 `json:"ratio"`
	Enabled      bool    `json:"enabled" gorm:"default:true;index"`
	CreatedAt    int64   `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    int64   `json:"updated_at" gorm:"autoUpdateTime"`
}

type modelPricingRuleKey struct {
	SubjectValue string
	Model        string
	UsingGroup   string
}

type modelPricingRuleIndex struct {
	byUser      map[modelPricingRuleKey]ModelPricingRule
	byUserGroup map[modelPricingRuleKey]ModelPricingRule
}

var modelPricingRuleCache atomic.Value

func init() {
	modelPricingRuleCache.Store(&modelPricingRuleIndex{
		byUser:      make(map[modelPricingRuleKey]ModelPricingRule),
		byUserGroup: make(map[modelPricingRuleKey]ModelPricingRule),
	})
}

func normalizeModelPricingRuleModel(name string) string {
	return ratio_setting.FormatMatchingModelName(strings.TrimSpace(name))
}

func modelPricingRuleKeyFor(subjectValue, modelName, usingGroup string) modelPricingRuleKey {
	return modelPricingRuleKey{
		SubjectValue: strings.TrimSpace(subjectValue),
		Model:        normalizeModelPricingRuleModel(modelName),
		UsingGroup:   strings.TrimSpace(usingGroup),
	}
}

func validateModelPricingRule(rule *ModelPricingRule) error {
	if rule == nil {
		return errors.New("model pricing rule is required")
	}
	rule.SubjectType = strings.TrimSpace(rule.SubjectType)
	rule.SubjectValue = strings.TrimSpace(rule.SubjectValue)
	rule.Model = normalizeModelPricingRuleModel(rule.Model)
	rule.UsingGroup = strings.TrimSpace(rule.UsingGroup)

	if rule.SubjectType != ModelPricingRuleSubjectUser && rule.SubjectType != ModelPricingRuleSubjectUserGroup {
		return errors.New("subject_type must be user or user_group")
	}
	if rule.SubjectValue == "" {
		return errors.New("subject_value is required")
	}
	if rule.SubjectType == ModelPricingRuleSubjectUser {
		userID, err := strconv.Atoi(rule.SubjectValue)
		if err != nil || userID <= 0 {
			return errors.New("user subject_value must be a positive user id")
		}
		rule.SubjectValue = strconv.Itoa(userID)
	}
	if len(rule.SubjectValue) > 64 || rule.Model == "" || len(rule.Model) > 255 || len(rule.UsingGroup) > 64 {
		return errors.New("model pricing rule contains an invalid subject, model, or using group")
	}
	if math.IsNaN(rule.Ratio) || math.IsInf(rule.Ratio, 0) || rule.Ratio < 0 {
		return errors.New("ratio must be a finite number not less than 0")
	}
	return nil
}

func validateModelPricingRuleSubject(rule *ModelPricingRule) error {
	if rule.SubjectType != ModelPricingRuleSubjectUser {
		return nil
	}
	userID, _ := strconv.Atoi(rule.SubjectValue)
	var user User
	err := DB.Select("id").First(&user, "id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %d", ErrModelPricingRuleUserNotFound, userID)
	}
	return err
}

func reloadModelPricingRuleCache(rules []ModelPricingRule) {
	index := &modelPricingRuleIndex{
		byUser:      make(map[modelPricingRuleKey]ModelPricingRule),
		byUserGroup: make(map[modelPricingRuleKey]ModelPricingRule),
	}
	for _, rule := range rules {
		if !rule.Enabled || validateModelPricingRule(&rule) != nil {
			continue
		}
		key := modelPricingRuleKeyFor(rule.SubjectValue, rule.Model, rule.UsingGroup)
		switch rule.SubjectType {
		case ModelPricingRuleSubjectUser:
			index.byUser[key] = rule
		case ModelPricingRuleSubjectUserGroup:
			index.byUserGroup[key] = rule
		}
	}
	modelPricingRuleCache.Store(index)
}

// ModelPricingRuleTablesAvailable lets replicas keep serving legacy pricing
// during a rolling upgrade, before the primary has completed the migration.
func ModelPricingRuleTablesAvailable() bool {
	return DB != nil && DB.Migrator().HasTable(&ModelPricingRule{})
}

// ReloadModelPricingRuleCache refreshes all local rule lookups from the DB.
func ReloadModelPricingRuleCache() error {
	if !ModelPricingRuleTablesAvailable() {
		return ErrModelPricingRuleTablesUnavailable
	}
	var rules []ModelPricingRule
	if err := DB.Order("id asc").Find(&rules).Error; err != nil {
		return err
	}
	reloadModelPricingRuleCache(rules)
	return nil
}

func SyncModelPricingRuleCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		if err := ReloadModelPricingRuleCache(); err != nil {
			// A failed periodic refresh must preserve the last known-good rules.
			continue
		}
	}
}

func GetModelPricingRules() ([]ModelPricingRule, error) {
	if !ModelPricingRuleTablesAvailable() {
		return nil, ErrModelPricingRuleTablesUnavailable
	}
	var rules []ModelPricingRule
	if err := DB.Order("subject_type asc, subject_value asc, model asc, using_group asc").Find(&rules).Error; err != nil {
		return nil, err
	}
	if err := populateModelPricingRuleSubjectNames(rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func populateModelPricingRuleSubjectNames(rules []ModelPricingRule) error {
	userIDs := make([]int, 0)
	seenUserIDs := make(map[int]struct{})
	for _, rule := range rules {
		if rule.SubjectType != ModelPricingRuleSubjectUser {
			continue
		}
		userID, err := strconv.Atoi(rule.SubjectValue)
		if err != nil || userID <= 0 {
			continue
		}
		if _, exists := seenUserIDs[userID]; exists {
			continue
		}
		seenUserIDs[userID] = struct{}{}
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) == 0 {
		return nil
	}

	var users []User
	if err := DB.Select("id, username").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return err
	}
	usernames := make(map[string]string, len(users))
	for _, user := range users {
		usernames[strconv.Itoa(user.Id)] = user.Username
	}
	for i := range rules {
		if rules[i].SubjectType == ModelPricingRuleSubjectUser {
			rules[i].SubjectName = usernames[rules[i].SubjectValue]
		}
	}
	return nil
}

func ensureModelPricingRuleScopeAvailableWithDB(db *gorm.DB, rule *ModelPricingRule) error {
	var existing ModelPricingRule
	query := db.Where("subject_type = ? AND subject_value = ? AND model = ? AND using_group = ?",
		rule.SubjectType, rule.SubjectValue, rule.Model, rule.UsingGroup)
	if rule.Id > 0 {
		query = query.Where("id <> ?", rule.Id)
	}
	err := query.First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrModelPricingRuleConflict
}

func ensureModelPricingRuleScopeAvailable(rule *ModelPricingRule) error {
	return ensureModelPricingRuleScopeAvailableWithDB(DB, rule)
}

func normalizeModelPricingRuleWriteError(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrModelPricingRuleConflict
	}
	return err
}

func CreateModelPricingRule(rule *ModelPricingRule) error {
	return CreateModelPricingRules([]*ModelPricingRule{rule})
}

// CreateModelPricingRules atomically creates one rule per model selection.
func CreateModelPricingRules(rules []*ModelPricingRule) error {
	if len(rules) == 0 {
		return errors.New("at least one model pricing rule is required")
	}

	seenScopes := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if err := validateModelPricingRule(rule); err != nil {
			return err
		}
		if err := validateModelPricingRuleSubject(rule); err != nil {
			return err
		}
		scope := strings.Join([]string{rule.SubjectType, rule.SubjectValue, rule.Model, rule.UsingGroup}, "\x00")
		if _, exists := seenScopes[scope]; exists {
			return ErrModelPricingRuleConflict
		}
		seenScopes[scope] = struct{}{}
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, rule := range rules {
			if err := ensureModelPricingRuleScopeAvailableWithDB(tx, rule); err != nil {
				return err
			}
			if err := tx.Create(rule).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return normalizeModelPricingRuleWriteError(err)
	}
	return ReloadModelPricingRuleCache()
}

func UpdateModelPricingRule(rule *ModelPricingRule) error {
	if rule == nil || rule.Id <= 0 {
		return errors.New("model pricing rule id is required")
	}
	if err := validateModelPricingRule(rule); err != nil {
		return err
	}
	if err := validateModelPricingRuleSubject(rule); err != nil {
		return err
	}
	var existing ModelPricingRule
	if err := DB.First(&existing, "id = ?", rule.Id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrModelPricingRuleNotFound
		}
		return err
	}
	if err := ensureModelPricingRuleScopeAvailable(rule); err != nil {
		return err
	}
	result := DB.Model(&ModelPricingRule{}).Where("id = ?", rule.Id).Updates(map[string]any{
		"subject_type":  rule.SubjectType,
		"subject_value": rule.SubjectValue,
		"model":         rule.Model,
		"using_group":   rule.UsingGroup,
		"ratio":         rule.Ratio,
		"enabled":       rule.Enabled,
	})
	if result.Error != nil {
		return normalizeModelPricingRuleWriteError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrModelPricingRuleNotFound
	}
	return ReloadModelPricingRuleCache()
}

func DeleteModelPricingRule(id int) error {
	if id <= 0 {
		return errors.New("model pricing rule id is required")
	}
	result := DB.Where("id = ?", id).Delete(&ModelPricingRule{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrModelPricingRuleNotFound
	}
	return ReloadModelPricingRuleCache()
}

// ResolveModelPricingRule gives user-specific rules precedence over user-group
// rules. For each subject, an exact route group wins over the route-agnostic
// rule. The caller applies legacy group ratios when no rule matches.
func ResolveModelPricingRule(userID int, userGroup, modelName, usingGroup string) (ModelPricingRule, bool) {
	index := modelPricingRuleCache.Load().(*modelPricingRuleIndex)
	modelName = normalizeModelPricingRuleModel(modelName)
	usingGroup = strings.TrimSpace(usingGroup)
	if modelName == "" {
		return ModelPricingRule{}, false
	}

	lookup := func(rules map[modelPricingRuleKey]ModelPricingRule, subjectValue string) (ModelPricingRule, bool) {
		if subjectValue == "" {
			return ModelPricingRule{}, false
		}
		if rule, ok := rules[modelPricingRuleKeyFor(subjectValue, modelName, usingGroup)]; ok {
			return rule, true
		}
		rule, ok := rules[modelPricingRuleKeyFor(subjectValue, modelName, "")]
		return rule, ok
	}

	if rule, ok := lookup(index.byUser, strconv.Itoa(userID)); ok {
		return rule, true
	}
	return lookup(index.byUserGroup, userGroup)
}
