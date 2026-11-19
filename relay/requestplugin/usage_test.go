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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

// Missing provider usage must behave identically with the policy enabled or
// disabled, including local estimation and the bytes sent to clients.
func TestUsageRewritePreservesMissingUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, tc := range []struct {
		name, path, body string
		format           types.RelayFormat
		stream           bool
		handler          func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{name: "Chat HTTP", path: "/v1/chat/completions", format: types.RelayFormatOpenAI, body: `{"id":"r1","model":"gpt","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`, handler: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
			return openai.OpenaiHandler(c, info, resp)
		}},
		{name: "Chat SSE", path: "/v1/chat/completions", format: types.RelayFormatOpenAI, stream: true, body: "data: {\"id\":\"r1\",\"created\":1,\"model\":\"gpt\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", handler: openai.OaiStreamHandler},
		{name: "Responses HTTP", path: "/v1/responses", format: types.RelayFormatOpenAIResponses, body: `{"id":"r1","object":"response","model":"gpt","status":"completed","output":[]}`, handler: openai.OaiResponsesHandler},
		{name: "Responses SSE", path: "/v1/responses", format: types.RelayFormatOpenAIResponses, stream: true, body: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[]}}\n\n", handler: openai.OaiResponsesStreamHandler},
		{name: "Claude HTTP", path: "/v1/messages", format: types.RelayFormatClaude, body: `{"id":"r1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`, handler: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
			return claude.ClaudeHandler(c, resp, info)
		}},
		{name: "Gemini HTTP", path: "/v1beta/models/gemini:generateContent", format: types.RelayFormatGemini, body: `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}]}`, handler: gemini.GeminiTextGenerationHandler},
		{name: "Gemini SSE", path: "/v1beta/models/gemini:streamGenerateContent", format: types.RelayFormatGemini, stream: true, body: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}]}\n\n", handler: gemini.GeminiTextGenerationStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var originalUsage *dto.Usage
			var originalBody []byte
			for _, enabled := range []bool{false, true} {
				instances := `[]`
				if enabled {
					instances = `[{"id":"rewrite","name":"Rewrite","plugin":"usage-rewrite","enabled":true,"global":true,"config":{"uncachedInputTokens":3,"outputPercent":0}}]`
				}
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: instances}))
				t.Cleanup(func() {
					require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
				})
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
				c.Set("group", "default")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt")
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
				_, _, apiErr := requestplugin.Ingress(c, tc.format)
				require.Nil(t, apiErr)
				info := &relaycommon.RelayInfo{OriginModelName: "gpt", RelayFormat: tc.format, RelayMode: relayconstant.RelayModeChatCompletions, IsStream: tc.stream, ShouldIncludeUsage: true}
				if tc.format == types.RelayFormatOpenAIResponses {
					info.RelayMode = relayconstant.RelayModeResponses
				}
				finish := requestplugin.Reply(c, tc.format, info)
				info.InitChannelMeta(c)
				info.UpstreamModelName = "gpt"
				info.SetEstimatePromptTokens(100)
				require.Nil(t, requestplugin.Egress(c, info, []byte(`{}`)))
				response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}
				usage, responseErr := tc.handler(c, info, response)
				finish()
				require.Nil(t, responseErr)
				require.NotNil(t, usage)
				if !enabled {
					originalUsage, originalBody = usage, recorder.Body.Bytes()
					continue
				}
				assert.Equal(t, originalUsage, usage)
				assert.Equal(t, string(originalBody), recorder.Body.String())
				assert.Nil(t, info.ResponseUsageOriginal)
				assert.Nil(t, info.ResponseUsageAudit)
			}
		})
	}
}

// Exercise the real response parser, wallet reservation, settlement and consume
// log together. No schema, SQL, or dialect behavior is changed by the policy.
func TestResponseUsageAccounting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	service.InitHttpClient()
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
		responses, stream    bool
		passthrough, bound   bool
		input, read, write   int
		output, cost         int
	}{
		{name: "OpenAI fixed uncached retention", plugin: "usage-rewrite", config: `{"uncachedInputTokens":3,"outputPercent":50}`, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Claude fixed uncached retention", plugin: "usage-rewrite", config: `{"uncachedInputTokens":3,"outputPercent":50}`, claude: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Responses group rule after request preparation", plugin: "usage-rewrite", config: `{"uncachedInputTokens":3,"outputPercent":50}`, responses: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Responses stream group rule after request preparation", plugin: "usage-rewrite", config: `{"uncachedInputTokens":3,"outputPercent":50}`, responses: true, stream: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Responses passthrough channel rule after request preparation", plugin: "usage-rewrite", config: `{"uncachedInputTokens":3,"outputPercent":50}`, responses: true, stream: true, passthrough: true, bound: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Responses legacy percentage after request preparation", plugin: "usage-rewrite", config: `{"uncachedInputPercent":30,"outputPercent":50}`, responses: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "Responses stream below threshold preserves original usage and charge", plugin: "usage-rewrite", config: `{"minInputTokens":101,"uncachedInputTokens":3,"outputPercent":50}`, responses: true, stream: true, input: 100, read: 89, output: 2000, cost: 60398},
		{name: "Responses at threshold rewrites usage and charge", plugin: "usage-rewrite", config: `{"minInputTokens":100,"uncachedInputTokens":3,"outputPercent":50}`, responses: true, input: 100, read: 89, write: 8, output: 1000, cost: 30278},
		{name: "changed input keeps real context tier", plugin: "rp-usage-accounting", config: `{"patch":{"inputTokens":50,"cacheReadTokens":39,"cacheWriteTokens":8,"outputTokens":1000}}`, input: 50, read: 39, write: 8, output: 1000, cost: 30178},
		{name: "explicit zero fully refunds reservation", plugin: "rp-usage-accounting", config: `{"patch":{"inputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0,"outputTokens":0}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const group = "伪装key"
			scope := `"global":true`
			if tc.responses {
				scope = `"groups":["` + group + `"]`
			}
			if tc.bound {
				scope = `"global":false`
			}
			instances := fmt.Sprintf(`[{"id":"rewrite","name":"Rewrite","plugin":%q,"enabled":true,%s,"config":%s}]`, tc.plugin, scope, tc.config)
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
			} else if tc.responses {
				format = types.RelayFormatOpenAIResponses
			}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: 1, UpstreamModelName: "gpt-up"}}
			info.ChannelId = channel.Id
			info.RelayFormat, info.FinalRequestRelayFormat = format, format
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.UserId, info.TokenId, info.TokenKey = user.Id, token.Id, token.Key
			info.UserGroup, info.UsingGroup = group, group
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
			c.Set("group", group)
			c.Set("id", user.Id)
			c.Set("token_id", token.Id)
			c.Set("token_name", token.Name)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt")
			common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			channelSettings := dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough}
			if tc.bound {
				channelSettings.RequestPlugins = []string{"rewrite"}
			}
			common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSettings)
			if tc.responses {
				info.RelayMode = relayconstant.RelayModeResponses
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt","input":"hello","stream":%t}`, tc.stream)))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
			}
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
			if tc.responses {
				payload = `{"id":"r1","object":"response","model":"gpt","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":2000,"total_tokens":2100,"input_tokens_details":{"cached_tokens":89}},"vendor_extra":{"keep":true}}`
				if tc.stream {
					payload = "data: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\n"
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.stream {
						w.Header().Set("Content-Type", "text/event-stream")
					} else {
						w.Header().Set("Content-Type", "application/json")
					}
					_, _ = io.WriteString(w, payload)
				}))
				t.Cleanup(upstream.Close)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				var request dto.OpenAIResponsesRequest
				request.Model, request.Input = "gpt", []byte(`"hello"`)
				request.Stream = common.GetPointer(tc.stream)
				info.Request, info.IsStream = &request, tc.stream
				adaptor, body, closer, prepareErr := relay.PrepareResponsesRequest(c, info, &request)
				require.Nil(t, prepareErr)
				t.Cleanup(func() { require.NoError(t, closer.Close()) })
				upstreamResponse, err := adaptor.DoRequest(c, info, body)
				require.NoError(t, err)
				responseUsage, responseErr := adaptor.DoResponse(c, upstreamResponse.(*http.Response), info)
				require.Nil(t, responseErr)
				usage, apiErr = responseUsage.(*dto.Usage), responseErr
			} else if tc.claude {
				info.InitChannelMeta(c)
				require.Nil(t, requestplugin.Egress(c, info, []byte(`{}`)))
				usage, apiErr = claude.ClaudeHandler(c, resp, info)
			} else {
				info.InitChannelMeta(c)
				require.Nil(t, requestplugin.Egress(c, info, []byte(`{}`)))
				usage, apiErr = openai.OpenaiHandler(c, info, resp)
			}
			finish()
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			canonical, ok := usage.BillingUsage.CanonicalUsage()
			if !ok {
				canonical = usage
			}
			assert.Equal(t, tc.input, canonical.InputTokens)
			assert.Equal(t, tc.output, canonical.CompletionTokens)
			assert.Equal(t, tc.read, canonical.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tc.write, canonical.PromptTokensDetails.CacheCreationTokensTotal())
			wire := recorder.Body.Bytes()
			if tc.responses {
				if tc.stream {
					wire = []byte(strings.TrimSpace(strings.TrimPrefix(string(wire), "data: ")))
					wire = []byte(gjson.GetBytes(wire, "response").Raw)
				}
				assert.EqualValues(t, tc.input, gjson.GetBytes(wire, "usage.input_tokens").Int())
				assert.EqualValues(t, tc.read, gjson.GetBytes(wire, "usage.input_tokens_details.cached_tokens").Int())
				assert.EqualValues(t, tc.write, gjson.GetBytes(wire, "usage.input_tokens_details.cache_write_tokens").Int())
				assert.EqualValues(t, tc.output, gjson.GetBytes(wire, "usage.output_tokens").Int())
			} else if tc.claude {
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
