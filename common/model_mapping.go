package common

import (
	"errors"
	"fmt"
	"strings"
)

type ModelMappingResolution struct {
	Origin string   `json:"origin"`
	Model  string   `json:"model"`
	Mapped bool     `json:"mapped"`
	Chain  []string `json:"chain"`
}

func ResolveModelMapping(modelMapping string, originModel string) (ModelMappingResolution, error) {
	originModel = strings.TrimSpace(originModel)
	result := ModelMappingResolution{
		Origin: originModel,
		Model:  originModel,
		Chain:  []string{originModel},
	}
	if originModel == "" || strings.TrimSpace(modelMapping) == "" || strings.TrimSpace(modelMapping) == "{}" {
		return result, nil
	}

	modelMap := make(map[string]string)
	if err := UnmarshalJsonStr(modelMapping, &modelMap); err != nil {
		return result, fmt.Errorf("unmarshal_model_mapping_failed: %w", err)
	}

	currentModel := originModel
	visitedModels := map[string]bool{currentModel: true}
	for {
		mappedModel, exists := modelMap[currentModel]
		mappedModel = strings.TrimSpace(mappedModel)
		if !exists || mappedModel == "" {
			break
		}
		if mappedModel == currentModel {
			break
		}
		if visitedModels[mappedModel] {
			return result, errors.New("model_mapping_contains_cycle")
		}
		visitedModels[mappedModel] = true
		currentModel = mappedModel
		result.Chain = append(result.Chain, currentModel)
		result.Mapped = true
	}
	result.Model = currentModel
	return result, nil
}
