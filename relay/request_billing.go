package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && constant.CountToken {
		meta = info.Request.GetTokenCountMeta()
	} else {
		// Avoid building CombineText when only the pricing quantities are needed.
		switch request := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			meta.MaxTokens = int(max(lo.FromPtr(request.MaxTokens), lo.FromPtr(request.MaxCompletionTokens)))
		case *dto.OpenAIResponsesRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxOutputTokens))
		case *dto.ClaudeRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxTokens))
		case *dto.ImageRequest:
			meta = request.GetTokenCountMeta()
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if info.IsPlayground && info.UsingGroup == service.ImageStudioFreeImageGroup {
		imageRequest, ok := info.Request.(*dto.ImageRequest)
		if !ok {
			return types.NewErrorWithStatusCode(errors.New("invalid image studio request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		count, countErr := imageRequest.ImageCount(false)
		if countErr != nil {
			return types.NewErrorWithStatusCode(countErr, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		reservation, reserveErr := service.ReserveImageStudioFreeImages(count)
		if reserveErr != nil {
			if errors.Is(reserveErr, service.ErrImageStudioInvalidImageCount) {
				return types.NewErrorWithStatusCode(errors.New("image count is invalid"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			statusCode := http.StatusServiceUnavailable
			message := "Image Studio free quota is temporarily unavailable"
			if errors.Is(reserveErr, service.ErrImageStudioQuotaExceeded) {
				statusCode = http.StatusTooManyRequests
				message = "Daily free image quota is insufficient"
			}
			return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, statusCode, types.ErrOptionWithSkipRetry())
		}
		info.ImageStudioQuotaReservation = reservation
		info.IsImageStudioFree = true
		return nil
	}
	if priceData.FreeModel {
		logger.LogInfo(c, common.LogText("model %s is free, skipping pre-consume", info.OriginModelName))
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// RefundFailedRequestBilling applies the common final-failure policy after all
// eligible attempts have ended. A settled BillingSession never refunds again.
func RefundFailedRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		return nil
	}
	if info != nil && info.ImageStudioQuotaReservation != nil {
		service.ReleaseImageStudioFreeImages(info.ImageStudioQuotaReservation)
		info.ImageStudioQuotaReservation = nil
		info.IsImageStudioFree = false
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	return apiErr
}
