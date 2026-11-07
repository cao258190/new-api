package controller

import (
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetAffiliateRewards lists only the authenticated inviter's settled rebates.
func GetAffiliateRewards(c *gin.Context) {
	sourceType := c.Query("source_type")
	if sourceType != "" && sourceType != model.AffiliateRewardSourceTopUp && sourceType != model.AffiliateRewardSourceRedemption {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	pageInfo := common.GetPageQuery(c)
	if pageInfo.Page < 1 || pageInfo.PageSize < 1 || pageInfo.Page > math.MaxInt/pageInfo.PageSize {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	rewards, total, err := model.GetAffiliateRewards(c.GetInt("id"), sourceType, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}
