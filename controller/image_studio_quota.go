package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetImageStudioQuota(c *gin.Context) {
	status, err := service.GetImageStudioQuotaStatus()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Image Studio quota is unavailable"))
		return
	}
	common.ApiSuccess(c, status)
}

func AdminGetImageStudioQuota(c *gin.Context) {
	GetImageStudioQuota(c)
}

func AdminUpdateImageStudioQuota(c *gin.Context) {
	var request struct {
		DailyLimit *int `json:"daily_limit"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.DailyLimit == nil || *request.DailyLimit < 0 || *request.DailyLimit > service.ImageStudioDailyFreeImageMax {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Daily free image limit must be between 0 and 1000000"))
		return
	}
	if err := model.UpdateOption(service.ImageStudioDailyFreeImageKey, strconv.Itoa(*request.DailyLimit)); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "image_studio.quota.update", map[string]any{"daily_limit": *request.DailyLimit})
	status, err := service.GetImageStudioQuotaStatus()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Image Studio quota is unavailable"))
		return
	}
	common.ApiSuccess(c, status)
}

func AdminResetImageStudioQuota(c *gin.Context) {
	if err := service.ResetImageStudioDailyFreeQuota(); err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Image Studio quota is unavailable"))
		return
	}
	recordManageAudit(c, "image_studio.quota.reset", map[string]any{})
	status, err := service.GetImageStudioQuotaStatus()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Image Studio quota is unavailable"))
		return
	}
	common.ApiSuccess(c, status)
}
