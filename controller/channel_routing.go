package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type updateVideoRoutingPolicyRequest struct {
	PublicModel string `json:"public_model"`
	Strict      bool   `json:"strict"`
	Revision    int    `json:"revision"`
}

type upsertVideoRoutingCapabilityRequest struct {
	ChannelId     int                      `json:"channel_id"`
	UpstreamModel string                   `json:"upstream_model"`
	Capability    dto.VideoModelCapability `json:"capability"`
	Revision      int                      `json:"revision"`
}

func GetChannelRoutingRules(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model is required"})
		return
	}
	rules, err := service.GetVideoRoutingRuleSetForPath(modelName, c.Query("group"), c.Query("request_path"))
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

func UpdateChannelRoutingPolicy(c *gin.Context) {
	var request updateVideoRoutingPolicyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	policy, err := service.UpsertVideoRoutingPolicy(request.PublicModel, request.Strict, request.Revision, c.GetInt("id"))
	if err != nil {
		respondVideoRoutingWriteError(c, err)
		return
	}
	recordManageAudit(c, "channel.routing_policy_update", map[string]interface{}{
		"model":    policy.PublicModel,
		"strict":   policy.Strict,
		"revision": policy.Revision,
	})
	common.ApiSuccess(c, policy)
}

func UpsertChannelRoutingCapability(c *gin.Context) {
	var request upsertVideoRoutingCapabilityRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	rule, err := service.UpsertChannelVideoRoutingCapabilityRule(
		request.ChannelId,
		request.UpstreamModel,
		request.Capability,
		request.Revision,
		c.GetInt("id"),
	)
	if err != nil {
		respondVideoRoutingWriteError(c, err)
		return
	}
	recordManageAudit(c, "channel.routing_capability_update", map[string]interface{}{
		"channel_id":     rule.ChannelId,
		"upstream_model": rule.UpstreamModel,
		"revision":       rule.Revision,
	})
	common.ApiSuccess(c, rule)
}

func DeleteChannelRoutingCapability(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid rule id"})
		return
	}
	revision, err := strconv.Atoi(c.Query("revision"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid revision"})
		return
	}
	rule, err := service.DeleteVideoRoutingCapabilityRule(id, revision)
	if err != nil {
		respondVideoRoutingWriteError(c, err)
		return
	}
	recordManageAudit(c, "channel.routing_capability_delete", map[string]interface{}{
		"channel_id":     rule.ChannelId,
		"upstream_model": rule.UpstreamModel,
		"revision":       rule.Revision,
	})
	common.ApiSuccess(c, rule)
}

func respondVideoRoutingWriteError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrVideoRoutingRevisionConflict) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "routing rule was modified by another administrator; refresh and try again",
		})
		return
	}
	common.ApiError(c, err)
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
