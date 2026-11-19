package requestplugin_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

// Exercise the real response parser, wallet reservation, settlement and consume
// log together. No schema, SQL, or dialect behavior is changed by the policy.
func TestResponseUsageAccounting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	plugin, err := jsplugin.DefaultRegistry.Register(`export const meta = {apiVersion: 1, key: "rp-usage-accounting", name: "Accounting policy", version: "1.0.0", author: {name: "Test"}};
export function usage(ctx) { return {action: "continue", usage: ctx.config.patch}; }`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key)) })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch, oldLogs := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldLogs
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	const expression = `len > 90 ? tier("long", p * 20 + cr * 2 + cc * 5 + c * 30) : tier("short", p)`
	const wallet = 2_000_000
	for index, tc := range []struct {
		name, plugin, config string
		claude               bool
		input, read, write   int
		output, cost         int
	}{
		{name: "OpenAI cache reclassification", plugin: "usage-rewrite", config: `{"uncachedInputPercent":30,"outputPercent":50}`, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Claude cache reclassification", plugin: "usage-rewrite", config: `{"uncachedInputPercent":30,"outputPercent":50}`, claude: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "changed input keeps real context tier", plugin: "rp-usage-accounting", config: `{"patch":{"inputTokens":50,"cacheReadTokens":39,"cacheWriteTokens":8,"outputTokens":1000}}`, input: 50, read: 39, write: 8, output: 1000, cost: 30178},
		{name: "explicit zero fully refunds reservation", plugin: "rp-usage-accounting", config: `{"patch":{"inputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0,"outputTokens":0}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instances := fmt.Sprintf(`[{"id":"rewrite","name":"Rewrite","plugin":%q,"enabled":true,"global":true,"config":%s}]`, tc.plugin, tc.config)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: instances}))
			t.Cleanup(func() {
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
			})
			user := model.User{Username: fmt.Sprintf("usage_policy_%d", index), AffCode: fmt.Sprintf("usage%d", index), Quota: wallet, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("usage-policy-%d", index), Name: "usage", RemainQuota: wallet, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Name: "usage", Key: "unused", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			request := &billingexpr.RequestInput{Body: []byte(`{}`)}
			cost, trace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: 100, Len: 100, C: 2000}, *request)
			require.NoError(t, err)
			reservation, err := billingexpr.QuotaRoundStrict(cost / 1_000_000 * common.QuotaPerUnit)
			require.NoError(t, err)
			format := types.RelayFormatOpenAI
			if tc.claude {
				format = types.RelayFormatClaude
			}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: 1, UpstreamModelName: "gpt-up"}}
			info.ChannelId = channel.Id
			info.RelayFormat, info.FinalRequestRelayFormat = format, format
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.UserId, info.TokenId, info.TokenKey = user.Id, token.Id, token.Key
			info.UserGroup, info.UsingGroup = "default", "default"
			info.UserSetting = dto.UserSetting{BillingPreference: "wallet_only"}
			info.ForcePreConsume, info.StartTime = true, time.Now()
			info.FirstResponseTime = info.StartTime
			info.PriceData = hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}
			info.BillingRequestInput = request
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), QuotaPerUnit: common.QuotaPerUnit, GroupRatio: 1, EstimatedTier: trace.MatchedTier, EstimatedBillingUnit: trace.BillingUnit, EstimatedQuotaAfterGroup: reservation}
			info.SetEstimatePromptTokens(100)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
			c.Set("group", "default")
			c.Set("id", user.Id)
			c.Set("token_id", token.Id)
			c.Set("token_name", token.Name)
			_, _, apiErr := requestplugin.Ingress(c, format)
			require.Nil(t, apiErr)
			require.Nil(t, service.PreConsumeBilling(c, reservation, info))
			var held model.User
			require.NoError(t, db.First(&held, user.Id).Error)
			assert.Equal(t, wallet-reservation, held.Quota)
			finish := requestplugin.Reply(c, format, info)
			payload := `{"id":"r1","model":"gpt","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":2000,"total_tokens":2100,"prompt_tokens_details":{"cached_tokens":89}},"vendor_extra":{"keep":true}}`
			if tc.claude {
				payload = `{"id":"r1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"cache_read_input_tokens":89,"output_tokens":2000},"vendor_extra":{"keep":true}}`
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}
			var usage *dto.Usage
			if tc.claude {
				usage, apiErr = claude.ClaudeHandler(c, resp, info)
			} else {
				usage, apiErr = openai.OpenaiHandler(c, info, resp)
			}
			finish()
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			canonical, ok := usage.BillingUsage.CanonicalUsage()
			require.True(t, ok)
			assert.Equal(t, tc.input, canonical.InputTokens)
			assert.Equal(t, tc.output, canonical.CompletionTokens)
			assert.Equal(t, tc.read, canonical.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tc.write, canonical.PromptTokensDetails.CacheCreationTokensTotal())
			wire := recorder.Body.Bytes()
			if tc.claude {
				assert.EqualValues(t, tc.input-tc.read-tc.write, gjson.GetBytes(wire, "usage.input_tokens").Int())
				assert.EqualValues(t, tc.read, gjson.GetBytes(wire, "usage.cache_read_input_tokens").Int())
				assert.EqualValues(t, tc.write, gjson.GetBytes(wire, "usage.cache_creation_input_tokens").Int())
				assert.EqualValues(t, tc.output, gjson.GetBytes(wire, "usage.output_tokens").Int())
			} else {
				assert.EqualValues(t, tc.input, gjson.GetBytes(wire, "usage.prompt_tokens").Int())
				assert.EqualValues(t, tc.read, gjson.GetBytes(wire, "usage.prompt_tokens_details.cached_tokens").Int())
				assert.EqualValues(t, tc.write, gjson.GetBytes(wire, "usage.prompt_tokens_details.cache_write_tokens").Int())
				assert.EqualValues(t, tc.output, gjson.GetBytes(wire, "usage.completion_tokens").Int())
			}
			assert.True(t, gjson.GetBytes(wire, "vendor_extra.keep").Bool())
			service.PostTextConsumeQuota(c, info, usage, nil)
			want, err := billingexpr.QuotaRoundStrict(float64(tc.cost) / 1_000_000 * common.QuotaPerUnit)
			require.NoError(t, err)
			require.NoError(t, info.Billing.Settle(want), "settling twice must not charge twice")
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, wallet-want, user.Quota)
			assert.Equal(t, wallet-want, token.RemainQuota)
			assert.Equal(t, want, user.UsedQuota)
			var logs []model.Log
			require.NoError(t, db.Where("user_id = ?", user.Id).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, want, logs[0].Quota)
			assert.Equal(t, tc.output, logs[0].CompletionTokens)
			assert.EqualValues(t, 100, gjson.Get(logs[0].Other, "tier_facts.len").Int())
			assert.EqualValues(t, 2000, gjson.Get(logs[0].Other, "admin_info.response_usage.before.outputTokens").Int())
			assert.EqualValues(t, tc.output, gjson.Get(logs[0].Other, "admin_info.response_usage.after.outputTokens").Int())
		})
	}
}
