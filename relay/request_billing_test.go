package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareRequestBillingDoesNotApplyFreeImageQuotaOutsidePlayground(t *testing.T) {
	server := miniredis.RunT(t)
	previousRedis, previousRDB := common.RedisEnabled, common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previousRDB
		common.RedisEnabled = previousRedis
	})

	quotaSettings := operation_setting.GetQuotaSetting()
	previousFreePreconsume := quotaSettings.EnableFreeModelPreConsume
	quotaSettings.EnableFreeModelPreConsume = false
	t.Cleanup(func() { quotaSettings.EnableFreeModelPreConsume = previousFreePreconsume })
	previousRatios := ratio_setting.ModelRatio2JSONString()
	ratios := ratio_setting.GetModelRatioCopy()
	ratios["unpriced-ordinary-image-model"] = 0
	ratioData, err := common.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratioData)))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios)) })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, service.ImageStudioFreeImageGroup)
	info := &relaycommon.RelayInfo{
		OriginModelName: "unpriced-ordinary-image-model",
		UserGroup:       "default",
		UsingGroup:      service.ImageStudioFreeImageGroup,
		Request:         &dto.ImageRequest{Model: "unpriced-ordinary-image-model", Prompt: "a tree"},
	}

	require.Nil(t, PrepareRequestBilling(ctx, info))
	assert.False(t, info.IsPlayground)
	assert.False(t, info.IsImageStudioFree)
	assert.Nil(t, info.ImageStudioQuotaReservation)
	status, err := service.GetImageStudioQuotaStatus()
	require.NoError(t, err)
	assert.Zero(t, status.Used)
}
