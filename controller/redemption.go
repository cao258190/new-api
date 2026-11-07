package controller

import (
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorT(c, "Payment, redemption, subscription, and invitation reward features are disabled. The administrator must confirm compliance terms before enabling them.")
		return
	}

	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if utf8.RuneCountInString(redemption.Name) == 0 || utf8.RuneCountInString(redemption.Name) > 20 {
		common.ApiErrorT(c, "Redemption code name length must be between 1-20")
		return
	}
	if redemption.Count <= 0 {
		common.ApiErrorT(c, "Redemption code count must be greater than 0")
		return
	}
	if redemption.Count > 100 {
		common.ApiErrorT(c, "Maximum 100 redemption codes can be generated at once")
		return
	}
	redemption.NormalizeType()
	var planTitle string
	if redemption.Type == model.RedemptionTypeSubscription {
		if redemption.PlanId <= 0 {
			common.ApiErrorT(c, "Subscription plan is required")
			return
		}
		var plan model.SubscriptionPlan
		if err := model.DB.Where("id = ?", redemption.PlanId).First(&plan).Error; err != nil {
			common.ApiErrorT(c, "Subscription plan not found")
			return
		}
		if !plan.Enabled {
			common.ApiErrorT(c, "Subscription plan is not enabled")
			return
		}
		planTitle = plan.Title
		redemption.Quota = 0
	} else {
		if redemption.Type != model.RedemptionTypeQuota {
			common.ApiErrorT(c, "Invalid redemption type")
			return
		}
		if redemption.Quota <= 0 {
			common.ApiErrorT(c, "Redemption quota must be positive")
			return
		}
		if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
			common.ApiError(c, err)
			return
		}
		redemption.PlanId = 0
	}
	if err := validateExpiredTime(redemption.ExpiredTime); err != nil {
		common.ApiError(c, err)
		return
	}
	var keys []string
	for i := 0; i < redemption.Count; i++ {
		key := common.GetUUID()
		cleanRedemption := model.Redemption{
			UserId:      c.GetInt("id"),
			Name:        redemption.Name,
			Key:         key,
			Type:        redemption.Type,
			PlanId:      redemption.PlanId,
			CreatedTime: common.GetTimestamp(),
			Quota:       redemption.Quota,
			ExpiredTime: redemption.ExpiredTime,
		}
		err = cleanRedemption.Insert()
		if err != nil {
			common.SysError(common.LogText("failed to insert redemption: %s", err.Error()))
			common.ApiErrorStatus(c, http.StatusOK, common.NewMessage("Failed to create redemption code, please try again later"), gin.H{"data": keys})
			return
		}
		keys = append(keys, key)
	}
	auditParams := map[string]any{
		"name":    redemption.Name,
		"count":   redemption.Count,
		"quota":   logger.FormatQuota(redemption.Quota),
		"type":    redemption.Type,
		"plan_id": redemption.PlanId,
	}
	if planTitle != "" {
		auditParams["plan_title"] = planTitle
	}
	recordManageAudit(c, "redemption.create", auditParams)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if statusOnly == "" {
		redemption.NormalizeType()
		if redemption.Type == model.RedemptionTypeSubscription {
			if redemption.PlanId <= 0 {
				common.ApiErrorT(c, "Subscription plan is required")
				return
			}
			var plan model.SubscriptionPlan
			if err := model.DB.Where("id = ?", redemption.PlanId).First(&plan).Error; err != nil {
				common.ApiErrorT(c, "Subscription plan not found")
				return
			}
			if !plan.Enabled && redemption.PlanId != cleanRedemption.PlanId {
				common.ApiErrorT(c, "Subscription plan is not enabled")
				return
			}
			redemption.Quota = 0
		} else {
			if redemption.Type != model.RedemptionTypeQuota {
				common.ApiErrorT(c, "Invalid redemption type")
				return
			}
			if redemption.Quota <= 0 {
				common.ApiErrorT(c, "Redemption quota must be positive")
				return
			}
			if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
				common.ApiError(c, err)
				return
			}
			redemption.PlanId = 0
		}
		if err := validateExpiredTime(redemption.ExpiredTime); err != nil {
			common.ApiError(c, err)
			return
		}
		// If you add more fields, please also update redemption.Update()
		cleanRedemption.Name = redemption.Name
		cleanRedemption.Type = redemption.Type
		cleanRedemption.PlanId = redemption.PlanId
		cleanRedemption.Quota = redemption.Quota
		cleanRedemption.ExpiredTime = redemption.ExpiredTime
	}
	if statusOnly != "" {
		cleanRedemption.Status = redemption.Status
	}
	err = cleanRedemption.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(expired int64) error {
	if expired != 0 && expired < common.GetTimestamp() {
		return common.NewMessage("Expiration time cannot be earlier than current time")
	}
	return nil
}

func DeleteRedemptionBatch(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	count, err := model.BatchDeleteRedemptions(request.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.delete_batch", map[string]any{
		"count":                    count,
		"total":                    len(request.Ids),
		"requested_redemption_ids": request.Ids,
	})
	common.ApiSuccess(c, count)
}
