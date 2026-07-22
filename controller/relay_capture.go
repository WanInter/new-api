package controller

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycapture "github.com/QuantumNous/new-api/relay/capture"

	"github.com/gin-gonic/gin"
)

func GetChannelRelayCapturePolicy(c *gin.Context) {
	channel, err := relayCaptureChannel(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, channel.GetOtherSettings().RelayCapture)
}

func UpdateChannelRelayCapturePolicy(c *gin.Context) {
	channel, err := relayCaptureChannel(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var policy dto.RelayCapturePolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := policy.Validate(); err != nil {
		common.ApiError(c, err)
		return
	}
	settings := channel.GetOtherSettings()
	settings.RelayCapture = &policy
	channel.SetOtherSettings(settings)
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("settings", channel.OtherSettings).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.relay_capture_update", map[string]interface{}{
		"id":        channel.Id,
		"enabled":   policy.Enabled,
		"protocols": policy.Protocols,
	})
	common.ApiSuccess(c, policy)
}

func ListRelayCaptures(c *gin.Context) {
	if !relaycapture.IsConfigured() {
		common.ApiErrorMsg(c, "relay capture storage is not configured")
		return
	}
	page := common.GetPageQuery(c)
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	result, err := relaycapture.GetStorage().List(c.Request.Context(), relaycapture.ListFilter{
		ChannelID: channelID,
		Protocol:  strings.TrimSpace(c.Query("protocol")),
		RequestID: strings.TrimSpace(c.Query("request_id")),
		Offset:    page.GetStartIdx(),
		Limit:     page.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(result.Total)
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}

func GetRelayCaptureMetadata(c *gin.Context) {
	metadata, err := getRelayCaptureMetadata(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, metadata)
}

func GetRelayCapturePart(c *gin.Context) {
	part := c.Param("part")
	if part != relaycapture.PartRequest && part != relaycapture.PartResponse {
		common.ApiError(c, fmt.Errorf("invalid relay capture part"))
		return
	}
	body, metadata, err := relaycapture.GetStorage().Open(c.Request.Context(), c.Param("id"), part)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer body.Close()
	contentType := metadata.Request.ContentType
	if part == relaycapture.PartResponse {
		contentType = metadata.Response.ContentType
	}
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.json", metadata.ID, part))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, body); err != nil {
		common.SysError("write relay capture part failed: " + err.Error())
	}
}

func DeleteOldRelayCaptures(c *gin.Context) {
	before, err := strconv.ParseInt(c.Query("before"), 10, 64)
	if err != nil || before <= 0 {
		common.ApiError(c, fmt.Errorf("before timestamp is required"))
		return
	}
	deleted, err := relaycapture.GetStorage().DeleteBefore(c.Request.Context(), before)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "relay_capture.delete_before", map[string]interface{}{"before": before, "count": deleted})
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}

func getRelayCaptureMetadata(c *gin.Context) (relaycapture.Metadata, error) {
	if !relaycapture.IsConfigured() {
		return relaycapture.Metadata{}, fmt.Errorf("relay capture storage is not configured")
	}
	result, err := relaycapture.GetStorage().List(c.Request.Context(), relaycapture.ListFilter{ID: c.Param("id"), Limit: 1})
	if err != nil {
		return relaycapture.Metadata{}, err
	}
	if len(result.Items) == 0 {
		return relaycapture.Metadata{}, fmt.Errorf("relay capture not found")
	}
	return result.Items[0], nil
}

func relayCaptureChannel(c *gin.Context) (*model.Channel, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("invalid channel id")
	}
	return model.GetChannelById(id, true)
}
