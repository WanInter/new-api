package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetTaskResultRehostSettings(c *gin.Context) {
	settings, err := service.GetTaskResultRehostSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    settings,
	})
}

func UpdateTaskResultRehostSettings(c *gin.Context) {
	var update service.TaskResultRehostSettingsUpdate
	if err := common.DecodeJson(c.Request.Body, &update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid task result storage settings",
		})
		return
	}
	settings, err := service.SaveTaskResultRehostSettings(c.Request.Context(), update)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "task result storage settings saved",
		"data":    settings,
	})
}

func TestTaskResultRehostSettings(c *gin.Context) {
	var update service.TaskResultRehostSettingsUpdate
	if err := common.DecodeJson(c.Request.Body, &update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid task result storage settings",
		})
		return
	}
	result, err := service.TestTaskResultRehostSettings(c.Request.Context(), update)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "object storage connection test succeeded",
		"data":    result,
	})
}
