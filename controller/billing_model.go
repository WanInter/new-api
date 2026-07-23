package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// BillingModelsUpdateRequest is deliberately map-based because the persisted
// configuration is model keyed. Supplying all three maps replaces the complete
// snapshot; omitting any map applies a model-level patch to the current
// snapshot. In both cases persistence publishes the three maps atomically.
type BillingModelsUpdateRequest struct {
	BillingMode   map[string]string `json:"billing_mode"`
	BillingExpr   map[string]string `json:"billing_expr"`
	BillingSchema map[string]string `json:"billing_schema"`
}

func GetBillingCapabilities(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}
	summary, err := relay.GetTaskBillingCapabilitySummary(modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    summary,
	})
}

func UpdateBillingModels(c *gin.Context) {
	request := BillingModelsUpdateRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	currentModes, currentExprs, currentSchemas := billing_setting.GetBillingSettingsCopy()
	if request.BillingMode == nil || request.BillingExpr == nil || request.BillingSchema == nil {
		request.BillingMode = mergeBillingModelMap(currentModes, request.BillingMode)
		request.BillingExpr = mergeBillingModelMap(currentExprs, request.BillingExpr)
		request.BillingSchema = mergeBillingModelMap(currentSchemas, request.BillingSchema)
	}

	modes, exprs, schemas, err := validateBillingModelMaps(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := model.UpdateBillingSettingMaps(modes, exprs, schemas); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "billing_models.update", map[string]interface{}{
		"model_count": len(modes),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"billing_mode":   modes,
			"billing_expr":   exprs,
			"billing_schema": schemas,
		},
	})
}

func validateBillingModelMaps(request BillingModelsUpdateRequest) (map[string]string, map[string]string, map[string]string, error) {
	modes := normalizeBillingMap(request.BillingMode)
	exprs := normalizeBillingMap(request.BillingExpr)
	schemas := normalizeBillingMap(request.BillingSchema)
	allModels := make(map[string]struct{}, len(modes)+len(exprs)+len(schemas))
	for modelName := range modes {
		allModels[modelName] = struct{}{}
	}
	for modelName := range exprs {
		allModels[modelName] = struct{}{}
	}
	for modelName := range schemas {
		allModels[modelName] = struct{}{}
	}

	for modelName := range allModels {
		mode := strings.TrimSpace(modes[modelName])
		if mode == "" {
			mode = billing_setting.BillingModeRatio
		}
		switch mode {
		case billing_setting.BillingModeRatio:
			modes[modelName] = mode
			delete(exprs, modelName)
			delete(schemas, modelName)
		case billing_setting.BillingModeTieredExpr:
			modes[modelName] = mode
			expression := strings.TrimSpace(exprs[modelName])
			if expression == "" {
				return nil, nil, nil, fmt.Errorf("模型 %s 已启用动态计费，但没有计费表达式", modelName)
			}
			exprs[modelName] = expression
			schemaVersion := strings.TrimSpace(schemas[modelName])
			if schemaVersion == "" {
				delete(schemas, modelName)
				if err := billing_setting.SmokeTestExpr(expression); err != nil {
					return nil, nil, nil, fmt.Errorf("模型 %s 的计费表达式无效: %w", modelName, err)
				}
				continue
			}
			schemas[modelName] = schemaVersion
			capability, err := relay.GetTaskBillingCapabilitySummary(modelName)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("读取模型 %s 的规范计费能力失败: %w", modelName, err)
			}
			if !capability.Compatible {
				return nil, nil, nil, fmt.Errorf("模型 %s 不能启用规范动态计费: %s", modelName, capability.Reason)
			}
			if capability.SchemaVersion != schemaVersion {
				return nil, nil, nil, fmt.Errorf("模型 %s 的 schema %q 与当前渠道能力 %q 不一致", modelName, schemaVersion, capability.SchemaVersion)
			}
			fields := make([]billingexpr.CanonicalBillingField, 0, len(capability.Fields))
			for _, field := range capability.Fields {
				fields = append(fields, billingexpr.CanonicalBillingField{
					Path:       field.Path,
					Type:       field.Type,
					Required:   field.Required,
					EnumValues: append([]string(nil), field.EnumValues...),
				})
			}
			if err := billingexpr.ValidateCanonicalBillingExpressionMatrix(expression, fields); err != nil {
				return nil, nil, nil, fmt.Errorf("模型 %s 的规范计费表达式无效: %w", modelName, err)
			}
		default:
			return nil, nil, nil, fmt.Errorf("模型 %s 的计费模式 %q 不受支持", modelName, mode)
		}
	}
	return modes, exprs, schemas, nil
}

func normalizeBillingMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		modelName := strings.TrimSpace(key)
		if modelName == "" {
			continue
		}
		result[modelName] = strings.TrimSpace(value)
	}
	return result
}

func mergeBillingModelMap(current, patch map[string]string) map[string]string {
	result := make(map[string]string, len(current)+len(patch))
	for modelName, value := range current {
		result[modelName] = value
	}
	for modelName, value := range patch {
		result[modelName] = value
	}
	return result
}
