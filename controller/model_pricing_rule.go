package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type modelPricingRuleRequest struct {
	SubjectType  string   `json:"subject_type"`
	SubjectValue string   `json:"subject_value"`
	Model        string   `json:"model"`
	Models       []string `json:"models"`
	UsingGroup   string   `json:"using_group"`
	Ratio        *float64 `json:"ratio"`
	Enabled      *bool    `json:"enabled"`
}

func modelPricingRulesFromRequest(request modelPricingRuleRequest) ([]*model.ModelPricingRule, error) {
	if request.Models == nil {
		rule, err := modelPricingRuleFromRequest(request)
		if err != nil {
			return nil, err
		}
		return []*model.ModelPricingRule{&rule}, nil
	}
	if len(request.Models) == 0 {
		return nil, errors.New("models is required")
	}

	rules := make([]*model.ModelPricingRule, 0, len(request.Models))
	for _, modelName := range request.Models {
		requestWithModel := request
		requestWithModel.Model = modelName
		rule, err := modelPricingRuleFromRequest(requestWithModel)
		if err != nil {
			return nil, err
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

func modelPricingRuleFromRequest(request modelPricingRuleRequest) (model.ModelPricingRule, error) {
	if request.Ratio == nil {
		return model.ModelPricingRule{}, errors.New("ratio is required")
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	return model.ModelPricingRule{
		SubjectType:  strings.TrimSpace(request.SubjectType),
		SubjectValue: strings.TrimSpace(request.SubjectValue),
		Model:        strings.TrimSpace(request.Model),
		UsingGroup:   strings.TrimSpace(request.UsingGroup),
		Ratio:        *request.Ratio,
		Enabled:      enabled,
	}, nil
}

func GetModelPricingRules(c *gin.Context) {
	rules, err := model.GetModelPricingRules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

func CreateModelPricingRule(c *gin.Context) {
	var request modelPricingRuleRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	rules, err := modelPricingRulesFromRequest(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.CreateModelPricingRules(rules); err != nil {
		respondModelPricingRuleWriteError(c, err)
		return
	}
	for _, rule := range rules {
		recordManageAudit(c, "model_pricing_rule.create", modelPricingRuleAuditParams(*rule))
	}
	if len(rules) == 1 {
		common.ApiSuccess(c, rules[0])
		return
	}
	common.ApiSuccess(c, rules)
}

func UpdateModelPricingRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid rule id"})
		return
	}
	var request modelPricingRuleRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	if request.Models != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "models is only supported when creating rules"})
		return
	}
	rule, err := modelPricingRuleFromRequest(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "ratio is required"})
		return
	}
	rule.Id = id
	if err := model.UpdateModelPricingRule(&rule); err != nil {
		respondModelPricingRuleWriteError(c, err)
		return
	}
	recordManageAudit(c, "model_pricing_rule.update", modelPricingRuleAuditParams(rule))
	common.ApiSuccess(c, rule)
}

func DeleteModelPricingRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid rule id"})
		return
	}
	if err := model.DeleteModelPricingRule(id); err != nil {
		respondModelPricingRuleWriteError(c, err)
		return
	}
	recordManageAudit(c, "model_pricing_rule.delete", map[string]interface{}{"id": id})
	common.ApiSuccess(c, gin.H{"id": id})
}

func respondModelPricingRuleWriteError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrModelPricingRuleNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
		return
	}
	if errors.Is(err, model.ErrModelPricingRuleConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
}

func modelPricingRuleAuditParams(rule model.ModelPricingRule) map[string]interface{} {
	return map[string]interface{}{
		"id":            rule.Id,
		"subject_type":  rule.SubjectType,
		"subject_value": rule.SubjectValue,
		"model":         rule.Model,
		"using_group":   rule.UsingGroup,
		"ratio":         rule.Ratio,
	}
}
