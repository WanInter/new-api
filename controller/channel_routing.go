package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelRoutingRules(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	rules, err := service.GetVideoRoutingRuleSet(modelName, c.Query("group"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

func SimulateChannelRouting(c *gin.Context) {
	var request service.VideoRoutingSimulationRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := validateVideoRoutingSimulationRequest(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(request.ContentType) == "" {
		request.ContentType = "application/json"
	}
	result, err := service.SimulateVideoRouting(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func validateVideoRoutingSimulationRequest(request service.VideoRoutingSimulationRequest) error {
	if strings.TrimSpace(request.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if strings.TrimSpace(request.Group) == "" {
		return fmt.Errorf("group is required")
	}
	if request.Images < 0 || request.Videos < 0 || request.Audios < 0 {
		return fmt.Errorf("media counts must be non-negative")
	}
	if request.Duration != nil && *request.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if request.Retry < 0 {
		return fmt.Errorf("retry must be non-negative")
	}
	return nil
}
