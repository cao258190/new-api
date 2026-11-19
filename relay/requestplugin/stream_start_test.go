package requestplugin_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/requestpatch"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
)

type startTransport func(*http.Request) (*http.Response, error)

func (f startTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// startRecorder lets delay tests inspect flushed output while the sender is
// still waiting upstream. Fake time alone does not synchronize buffer access.
type startRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *startRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(data)
}

func (r *startRecorder) WriteString(data string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.WriteString(data)
}

func (r *startRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.Flush()
}

func (r *startRecorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

// startFixture follows the controller's ingress -> info -> reply -> egress order.
func startFixture(t *testing.T, timing string, responses bool, extra ...string) (*gin.Context, *relaycommon.RelayInfo, *startRecorder, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedis })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	pluginConfig := `{}`
	if timing != "" {
		pluginConfig = fmt.Sprintf(`{"timing":%q,"delayMinSeconds":0,"delayMaxSeconds":0}`, timing)
		if strings.HasPrefix(timing, "{") {
			pluginConfig = timing
		}
	}
	// Existing start-event regressions exercise the original wire format;
	// output-item compatibility cases opt in explicitly below.
	var savedConfig map[string]any
	require.NoError(t, common.UnmarshalJsonStr(pluginConfig, &savedConfig))
	if _, configured := savedConfig["responsesOutputItem"]; !configured {
		savedConfig["responsesOutputItem"] = false
	}
	if _, configured := savedConfig["multiHopCompatibility"]; !configured {
		savedConfig["multiHopCompatibility"] = false
	}
	encodedConfig, err := common.Marshal(savedConfig)
	require.NoError(t, err)
	pluginConfig = string(encodedConfig)
	instances := fmt.Sprintf(`[{"id":"early","name":"首字优化","plugin":"first-token-optimization","enabled":true,"global":true,"config":%s}]`, pluginConfig)
	if len(extra) > 0 {
		instances = strings.TrimSuffix(instances, "]") + "," + strings.Join(extra, ",") + "]"
	}
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: instances}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
	})
	oldPing := operation_setting.GetGeneralSetting().PingIntervalEnabled
	operation_setting.GetGeneralSetting().PingIntervalEnabled = false
	t.Cleanup(func() { operation_setting.GetGeneralSetting().PingIntervalEnabled = oldPing })
	service.InitHttpClient()
	rec := &startRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	path := "/v1/chat/completions"
	if responses {
		path = "/v1/responses"
	}
	body := `{"model":"gpt-test","stream":true,"input":"hello","messages":[{"role":"user","content":"hello"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 101)
	c.Set("token_id", 202)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-test")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 7)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-test-credential")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.test")
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now().Add(-100*time.Millisecond))
	_, _, apiErr := requestplugin.Ingress(c, types.RelayFormatOpenAI)
	require.Nil(t, apiErr)
	var info *relaycommon.RelayInfo
	if responses {
		info = relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Model: "gpt-test", Stream: common.GetPointer(true)})
	} else {
		info = relaycommon.GenRelayInfoOpenAI(c, &dto.GeneralOpenAIRequest{Model: "gpt-test", Stream: common.GetPointer(true)})
	}
	finish := requestplugin.Reply(c, info.RelayFormat, info)
	info.InitChannelMeta(c)
	require.Nil(t, requestplugin.Egress(c, info, []byte(body)))
	t.Cleanup(func() {
		if stored, exists := c.Get(common.KeyBodyStorage); exists {
			_ = stored.(common.BodyStorage).Close()
		}
	})
	return c, info, rec, finish
}

func startRequest(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://upstream.test/v1/responses", strings.NewReader(`{}`))
	require.NoError(t, err)
	return channel.DoRequest(c, req, info)
}

// startEmptyRequest emits the configured start without a real network request.
func startEmptyRequest(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo) {
	t.Helper()
	previous := service.GetHttpClient().Transport
	defer func() { service.GetHttpClient().Transport = previous }()
	service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	resp, err := startRequest(t, c, info)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func startEvents(body string) []gjson.Result {
	var events []gjson.Result
	for line := range strings.SplitSeq(body, "\n") {
		if raw, ok := strings.CutPrefix(line, "data: "); ok && gjson.Valid(raw) {
			events = append(events, gjson.Parse(raw))
		}
	}
	return events
}

func TestStreamStartTimingAndUpstreamMeasurement(t *testing.T) {
	for _, timing := range []string{"", "before_upstream", "after_connect"} {
		for _, responses := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/responses=%t", timing, responses), func(t *testing.T) {
				c, info, rec, finish := startFixture(t, timing, responses)
				defer finish()
				expectedTiming := timing
				if expectedTiming == "" {
					expectedTiming = "after_connect"
				}
				originalFirst := info.FirstResponseTime
				service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, expectedTiming == "before_upstream", rec.Flushed, "only before_upstream emits before Do")
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
				})
				resp, err := startRequest(t, c, info)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.True(t, rec.Flushed, "start was flushed before reading the body")
				events := startEvents(rec.Body.String())
				require.Len(t, events, 1)
				if responses {
					assert.Equal(t, "response.created", events[0].Get("type").Str)
					assert.NotEmpty(t, events[0].Get("response.id").Str)
					assert.Equal(t, "in_progress", events[0].Get("response.status").Str)
				} else {
					assert.Equal(t, "assistant", events[0].Get("choices.0.delta.role").Str)
					assert.Empty(t, events[0].Get("choices.0.delta.content").Str)
				}
				assert.Equal(t, originalFirst, info.FirstResponseTime)
				assert.False(t, info.HasSendResponse(), "synthetic frames do not mark the upstream as successful")
				assert.Equal(t, expectedTiming, info.StreamStartTiming)
				info.SetFirstResponseTime()
				assert.True(t, info.HasSendResponse())
				other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1).Snapshot()
				admin := other["admin_info"].(map[string]any)["stream_start"].(map[string]any)
				assert.Equal(t, other["frt"], float64(admin["client_first_response_ms"].(int64)))
				assert.Equal(t, info.FirstResponseTime.Sub(info.StartTime).Milliseconds(), admin["upstream_first_response_ms"])
			})
		}
	}
}

func TestStreamStartDelayConfig(t *testing.T) {
	plugin, found := jsplugin.DefaultRegistry.GetIgnoringMasterSwitch("first-token-optimization")
	require.True(t, found)
	for _, tc := range []struct {
		name, config string
		minMS, maxMS int
	}{
		{"default timing stays after connection", `{}`, -1, -1},
		{"old before-upstream config gets default jitter", `{"timing":"before_upstream"}`, 1000, 2000},
		{"custom interval", `{"timing":"before_upstream","delayMinSeconds":0.25,"delayMaxSeconds":0.75}`, 250, 750},
		{"fixed fractional delay", `{"timing":"before_upstream","delayMinSeconds":1.25,"delayMaxSeconds":1.25}`, 1250, 1250},
		{"zero restores immediate emission", `{"timing":"before_upstream","delayMinSeconds":0,"delayMaxSeconds":0}`, 0, 0},
		{"after connection ignores hidden delay", `{"timing":"after_connect","delayMinSeconds":1,"delayMaxSeconds":2}`, -1, -1},
		{"output item can be disabled", `{"timing":"after_connect","responsesOutputItem":false}`, -1, -1},
		{"multi-hop can be disabled", `{"timing":"after_connect","multiHopCompatibility":false}`, -1, -1},
		{"disabled output item ignores saved multi-hop mode", `{"timing":"after_connect","responsesOutputItem":false,"multiHopCompatibility":true}`, -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validated, err := plugin.ValidateConfig([]byte(tc.config))
			require.NoError(t, err)
			result, err := plugin.CallTransform(t.Context(), jsplugin.TransformContext{
				Stage: "egress", Format: "openai", Path: "/v1/chat/completions", Config: validated,
			}, jsplugin.TransformRequest{Body: []byte(`{"stream":true}`)})
			require.NoError(t, err)
			effects, err := requestpatch.DecodeTransformEffects(result.Effects, requestpatch.EffectStreamStart)
			require.NoError(t, err)
			require.Len(t, effects, 1)
			assert.Equal(t, !gjson.Get(tc.config, "responsesOutputItem").Exists() || gjson.Get(tc.config, "responsesOutputItem").Bool(), effects[0].OutputItem)
			assert.Equal(t, effects[0].OutputItem && (!gjson.Get(tc.config, "multiHopCompatibility").Exists() || gjson.Get(tc.config, "multiHopCompatibility").Bool()), effects[0].CompleteOutputItem)
			if tc.minMS < 0 {
				assert.Equal(t, "after_connect", effects[0].Timing)
				assert.False(t, gjson.GetBytes(result.Effects, "0.delayMs").Exists())
				return
			}
			assert.Equal(t, "before_upstream", effects[0].Timing)
			assert.GreaterOrEqual(t, effects[0].DelayMS, tc.minMS)
			assert.LessOrEqual(t, effects[0].DelayMS, tc.maxMS)
		})
	}
	for _, raw := range []string{
		`{"delayMinSeconds":-1}`, `{"delayMaxSeconds":61}`, `{"delayMinSeconds":"1"}`,
		`{"delayMaxSeconds":null}`, `{"delayMinSeconds":3,"delayMaxSeconds":2}`,
		`{"multiHopCompatibility":"true"}`, `{"multiHopCompatibility":null}`,
	} {
		_, err := plugin.ValidateConfig([]byte(raw))
		require.Error(t, err, raw)
	}
}

func TestStreamStartDelayDoesNotDelayUpstreamOrPayload(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, variant := range []string{"slow headers", "slow body", "fast body", "upstream rejection", "nonstream response", "connection failure", "canceled"} {
			t.Run(fmt.Sprintf("%s/responses=%t", variant, responses), func(t *testing.T) {
				c, info, rec, finish := startFixture(t, `{"timing":"before_upstream","delayMinSeconds":1.5,"delayMaxSeconds":1.5}`, responses)
				defer finish()
				synctest.Test(t, func(t *testing.T) {
					startedAt := time.Now()
					ctx, cancel := context.WithCancel(c.Request.Context())
					defer cancel()
					c.Request = c.Request.WithContext(ctx)
					payload := "data: {\"id\":\"real-response\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\n\n"
					if responses {
						payload = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_real_delay\"}}\n\n"
					}
					started := make(chan struct{})
					service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
						assert.Equal(t, startedAt, time.Now(), "upstream starts without waiting for the start event")
						close(started)
						if variant == "canceled" {
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						if variant == "slow headers" {
							time.Sleep(5 * time.Second)
						} else if variant != "slow body" {
							time.Sleep(500 * time.Millisecond)
						}
						if variant == "connection failure" {
							return nil, errors.New("connection refused")
						}
						resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Request: req, Body: io.NopCloser(strings.NewReader(payload))}
						if variant == "upstream rejection" {
							resp.StatusCode = http.StatusTooManyRequests
						}
						if variant == "nonstream response" {
							resp.Header.Set("Content-Type", "application/json")
						}
						if variant == "slow body" {
							reader, writer := io.Pipe()
							resp.Body = reader
							go func() {
								time.Sleep(5 * time.Second)
								_, _ = io.WriteString(writer, payload)
								_ = writer.Close()
							}()
						}
						return resp, nil
					})
					type reply struct {
						response *http.Response
						err      error
					}
					done := make(chan reply, 1)
					go func() {
						resp, err := startRequest(t, c, info)
						done <- reply{resp, err}
					}()
					<-started
					synctest.Wait()
					assert.Empty(t, rec.snapshot())
					if variant == "canceled" {
						cancel()
					}
					slow := variant == "slow headers" || variant == "slow body"
					if slow {
						time.Sleep(1499 * time.Millisecond)
						synctest.Wait()
						assert.Empty(t, rec.snapshot())
						time.Sleep(time.Millisecond)
						synctest.Wait()
						assert.Len(t, startEvents(rec.snapshot()), 1)
						assert.Equal(t, 1500*time.Millisecond, info.ClientFirstResponseTime.Sub(startedAt))
						assert.False(t, info.HasSendResponse(), "synthetic output leaves genuine upstream timing alone")
					}
					got := <-done
					if variant == "canceled" || variant == "connection failure" {
						require.Error(t, got.err)
						assert.Empty(t, rec.snapshot())
					} else {
						require.NoError(t, got.err)
						raw, err := io.ReadAll(got.response.Body)
						require.NoError(t, err)
						assert.Equal(t, payload, string(raw), "the prefetched byte is restored without changing the body")
						assert.NoError(t, got.response.Request.Context().Err(), "body stays readable after the sender returns")
						require.NoError(t, got.response.Body.Close())
						assert.ErrorIs(t, got.response.Request.Context().Err(), context.Canceled)
						if !slow {
							assert.Empty(t, rec.snapshot())
							assert.True(t, info.ClientFirstResponseTime.IsZero())
							assert.Equal(t, 500*time.Millisecond, time.Since(startedAt))
						}
					}
					before := rec.snapshot()
					time.Sleep(3 * time.Second)
					synctest.Wait()
					assert.Equal(t, before, rec.snapshot(), "no delayed writes survive request completion")
				})
			})
		}
	}
}

func TestStreamStartDelayedBodyKeepsTimeoutAndCancellation(t *testing.T) {
	for _, clientGone := range []bool{false, true} {
		t.Run(fmt.Sprintf("clientGone=%t", clientGone), func(t *testing.T) {
			c, info, rec, finish := startFixture(t, `{"timing":"before_upstream","delayMinSeconds":1.5,"delayMaxSeconds":1.5}`, false)
			defer finish()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(c.Request.Context())
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				reader, writer := io.Pipe()
				defer writer.Close()
				service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: req}, nil
				})
				resp, err := startRequest(t, c, info)
				require.NoError(t, err)
				assert.Len(t, startEvents(rec.Body.String()), 1)
				if clientGone {
					go func() {
						time.Sleep(500 * time.Millisecond)
						cancel()
					}()
				}
				helper.StreamScannerHandler(c, resp, info, func(string, *helper.StreamResult) {})
				if clientGone {
					assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
				} else {
					assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
				}
				assert.ErrorIs(t, resp.Request.Context().Err(), context.Canceled)
				assert.Len(t, startEvents(rec.Body.String()), 1)
			})
		})
	}
}

func TestStreamStartResponsesPreservePayloadAndContinuation(t *testing.T) {
	c, info, rec, finish := startFixture(t, "before_upstream", true)
	defer finish()
	service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Request: req,
			Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_real\",\"model\":\"gpt-test\"}}\n\n" +
				"data: {\"type\":\"response.output_item.added\",\"sequence_number\":1,\"response_id\":\"resp_real\",\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"weather\",\"arguments\":\"{}\"}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"sequence_number\":2,\"item_id\":\"msg_1\",\"delta\":\"你好🙂\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"sequence_number\":9,\"response\":{\"id\":\"resp_real\",\"model\":\"gpt-test\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"total_tokens\":120}}}\n\n"))}, nil
	})
	resp, err := startRequest(t, c, info)
	require.NoError(t, err)
	usage, apiErr := openai.OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Nil(t, requestplugin.FinishStreamStart(c))
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 20, usage.CompletionTokens)
	events := startEvents(rec.Body.String())
	require.Len(t, events, 4)
	id := events[0].Get("response.id").Str
	assert.Equal(t, id, events[3].Get("response.id").Str)
	assert.Equal(t, id, events[1].Get("response_id").Str)
	assert.Equal(t, "fc_1", events[1].Get("item.id").Str)
	assert.Equal(t, "call_1", events[1].Get("item.call_id").Str)
	assert.Equal(t, "你好🙂", events[2].Get("delta").Str)
	for i, event := range events {
		assert.Equal(t, int64(i), event.Get("sequence_number").Int())
	}
	assert.Equal(t, int64(120), events[3].Get("response.usage.total_tokens").Int())
	assert.False(t, service.ShouldRetryRelayError(c, types.NewErrorWithStatusCode(errors.New("bad gateway"), types.ErrorCodeBadResponse, 502), 2))

	// Exercise the existing routing-access check with a minimal real ability.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	previousDB, previousMemory, previousRedis := model.DB, common.MemoryCacheEnabled, common.RedisEnabled
	previousDialect := common.MainDatabaseType()
	model.DB, common.MemoryCacheEnabled, common.RedisEnabled = db, true, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled, common.RedisEnabled = previousDB, previousMemory, previousRedis
		common.SetDatabaseTypes(previousDialect, common.LogDatabaseType())
	})
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Channel{}))
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-test", ChannelId: 7, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 7, Group: "default", Models: "gpt-test", Status: common.ChannelStatusEnabled}).Error)
	model.InitChannelCache()
	for _, tc := range []struct {
		name         string
		user, token  int
		model, group string
		ok           bool
		pin          int
		socket       bool
	}{
		{name: "owner", user: 101, token: 202, model: "gpt-test", group: "default", ok: true},
		{name: "other user", user: 102, token: 202, model: "gpt-test", group: "default"},
		{name: "other token", user: 101, token: 203, model: "gpt-test", group: "default"},
		{name: "other model", user: 101, token: 202, model: "gpt-other", group: "default"},
		{name: "other group", user: 101, token: 202, model: "gpt-test", group: "other"},
		{name: "conflicting channel pin", user: 101, token: 202, model: "gpt-test", group: "default", pin: 8},
		{name: "socket after disabling plugin", user: 101, token: 202, model: "gpt-test", group: "default", ok: true, socket: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, _ := gin.CreateTestContext(httptest.NewRecorder())
			request.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"previous_response_id":%q}`, tc.model, id)))
			request.Request.Header.Set("Content-Type", "application/json")
			request.Set("id", tc.user)
			request.Set("token_id", tc.token)
			common.SetContextKey(request, constant.ContextKeyUsingGroup, tc.group)
			common.SetContextKey(request, constant.ContextKeyOriginalModel, tc.model)
			t.Cleanup(func() { common.CleanupBodyStorage(request) })
			if tc.pin != 0 {
				service.GetChannelConstraints(request).AddPin(appdto.ChannelPin{ChannelId: tc.pin, Source: "token", Rank: appdto.PinRankToken})
			}
			var changed bool
			var aliasErr *types.NewAPIError
			if tc.socket {
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
				changed, _, _, aliasErr = requestplugin.SocketIngress(request, types.RelayFormatOpenAIResponses)
			} else {
				changed, aliasErr = service.ResolveResponseAlias(request, tc.model)
			}
			if !tc.ok {
				require.NotNil(t, aliasErr)
				assert.False(t, changed)
				return
			}
			require.Nil(t, aliasErr)
			assert.True(t, changed)
			storage, err := common.GetBodyStorage(request)
			require.NoError(t, err)
			raw, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, "resp_real", gjson.GetBytes(raw, "previous_response_id").Str)
			pin, pinned, _ := service.GetChannelConstraints(request).ResolvedPin()
			assert.True(t, pinned)
			assert.Equal(t, 7, pin.ChannelId)
			upstream := &model.Channel{Id: 7, Key: info.ApiKey, BaseURL: common.GetPointer(info.ChannelBaseUrl)}
			key, _, keyErr := service.ResponseContinuationKey(request, upstream)
			require.Nil(t, keyErr)
			assert.Equal(t, info.ApiKey, key)
			require.Nil(t, service.CheckResponseContinuationConnection(request, upstream, key))
			require.NotNil(t, service.CheckResponseContinuationConnection(request, upstream, "different-account"))
			upstream.ChannelInfo.IsMultiKey = true
			upstream.Key = info.ApiKey + "\nother-account"
			key, index, keyErr := service.ResponseContinuationKey(request, upstream)
			require.Nil(t, keyErr)
			assert.Equal(t, 0, index)
			assert.Equal(t, info.ApiKey, key)
			upstream.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
			_, _, keyErr = service.ResponseContinuationKey(request, upstream)
			require.NotNil(t, keyErr)
			upstream.ChannelInfo.IsMultiKey = false
			upstream.Key = "rotated"
			_, _, keyErr = service.ResponseContinuationKey(request, upstream)
			require.NotNil(t, keyErr)
		})
	}
}

func TestStreamStartResponsesOutputItem(t *testing.T) {
	for _, tc := range []struct {
		name, item, deltaType string
	}{
		{"text", `{"id":"msg_real","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"你好🙂","annotations":[]}]}`, "response.output_text.delta"},
		{"reasoning", `{"id":"rs_real","type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"思考内容"}],"encrypted_content":"preserved"}`, "response.reasoning_summary_text.delta"},
		{"tool", `{"id":"fc_real","type":"function_call","status":"completed","call_id":"call_real","name":"weather","arguments":"{}"}`, "response.function_call_arguments.delta"},
	} {
		for _, multiHop := range []bool{false, true} {
			for _, timing := range []string{"before_upstream", "after_connect"} {
				t.Run(fmt.Sprintf("%s/%s/multiHop=%t", tc.name, timing, multiHop), func(t *testing.T) {
					c, info, rec, finish := startFixture(t, fmt.Sprintf(`{"timing":%q,"delayMinSeconds":0,"delayMaxSeconds":0,"responsesOutputItem":true,"multiHopCompatibility":%t}`, timing, multiHop), true)
					defer finish()
					added, err := sjson.Set(tc.item, "status", "in_progress")
					require.NoError(t, err)
					itemID := gjson.Get(tc.item, "id").Str
					payload := fmt.Sprintf("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_real\",\"output\":[]}}\n\n"+
						"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":%s}\n\n"+
						"data: {\"type\":%q,\"output_index\":0,\"item_id\":%q,\"delta\":\"真实内容\",\"vendor_extra\":true}\n\n"+
						"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":%s}\n\n"+
						"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_real\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"total_tokens\":120}}}\n\n",
						added, tc.deltaType, itemID, tc.item, tc.item)
					service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Request: req, Body: io.NopCloser(strings.NewReader(payload))}, nil
					})
					resp, err := startRequest(t, c, info)
					require.NoError(t, err)
					prefix := startEvents(rec.snapshot())
					prefixCount := 2
					if multiHop {
						prefixCount = 3
					}
					require.Len(t, prefix, prefixCount, "early events are flushed before the upstream body is read")
					assert.Equal(t, "response.created", prefix[0].Get("type").Str)
					assert.Equal(t, "response.output_item.added", prefix[1].Get("type").Str)
					assert.Equal(t, "[]", prefix[1].Get("item.content").Raw)
					assert.Equal(t, "in_progress", prefix[1].Get("item.status").Str)
					if multiHop {
						assert.Equal(t, "response.output_item.done", prefix[2].Get("type").Str)
						assert.Equal(t, prefix[1].Get("item.id").Str, prefix[2].Get("item.id").Str)
						assert.Equal(t, "completed", prefix[2].Get("item.status").Str)
						assert.Equal(t, "[]", prefix[2].Get("item.content").Raw)
					}
					usage, apiErr := openai.OaiResponsesStreamHandler(c, info, resp)
					require.Nil(t, apiErr)
					require.Nil(t, requestplugin.FinishStreamStart(c))
					assert.Equal(t, 100, usage.PromptTokens)
					assert.Equal(t, 20, usage.CompletionTokens)
					events := startEvents(rec.snapshot())
					require.Len(t, events, 7)
					for i, event := range events {
						assert.Equal(t, int64(i), event.Get("sequence_number").Int())
					}
					placeholder := events[2].Get("item")
					assert.Equal(t, "response.output_item.done", events[2].Get("type").Str)
					assert.Equal(t, prefix[1].Get("item.id").Str, placeholder.Get("id").Str)
					assert.Equal(t, "completed", placeholder.Get("status").Str)
					assert.Equal(t, int64(1), events[3].Get("output_index").Int())
					assert.JSONEq(t, added, events[3].Get("item").Raw)
					assert.Equal(t, int64(1), events[4].Get("output_index").Int())
					assert.Equal(t, itemID, events[4].Get("item_id").Str)
					assert.Equal(t, "真实内容", events[4].Get("delta").Str)
					assert.True(t, events[4].Get("vendor_extra").Bool())
					assert.JSONEq(t, tc.item, events[5].Get("item").Raw)
					assert.JSONEq(t, placeholder.Raw, events[6].Get("response.output.0").Raw)
					assert.JSONEq(t, tc.item, events[6].Get("response.output.1").Raw)
					assert.Equal(t, int64(120), events[6].Get("response.usage.total_tokens").Int())

					// A later request must not send gateway-only empty items upstream,
					// including after disabling the plugin. Real text and IDs survive.
					require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
					request, _ := gin.CreateTestContext(httptest.NewRecorder())
					body := fmt.Sprintf(`{"model":"gpt-test","input":[%s,%s,{"type":"item_reference","id":%q},{"type":"message","id":%q,"role":"assistant","content":[{"type":"output_text","text":"保留"}]},{"type":"item_reference","id":%q},{"role":"user","content":"继续"}]}`, placeholder.Raw, tc.item, placeholder.Get("id").Str, placeholder.Get("id").Str, itemID)
					request.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
					request.Request.Header.Set("Content-Type", "application/json")
					t.Cleanup(func() { common.CleanupBodyStorage(request) })
					changed, apiErr := service.ResolveResponseAlias(request, "gpt-test")
					require.Nil(t, apiErr)
					assert.True(t, changed)
					storage, err := common.GetBodyStorage(request)
					require.NoError(t, err)
					raw, err := storage.Bytes()
					require.NoError(t, err)
					input := gjson.GetBytes(raw, "input").Array()
					require.Len(t, input, 4)
					assert.JSONEq(t, tc.item, input[0].Raw)
					assert.Equal(t, "保留", input[1].Get("content.0.text").Str)
					assert.Equal(t, itemID, input[2].Get("id").Str)
					assert.Equal(t, "继续", input[3].Get("content").Str)
					changed, apiErr = service.ResolveResponseAlias(request, "gpt-test")
					require.Nil(t, apiErr)
					assert.False(t, changed, "input cleanup is idempotent")
				})
			}
		}
	}
	for _, multiHop := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure/multiHop=%t", multiHop), func(t *testing.T) {
			c, info, rec, finish := startFixture(t, fmt.Sprintf(`{"timing":"before_upstream","delayMinSeconds":0,"delayMaxSeconds":0,"responsesOutputItem":true,"multiHopCompatibility":%t}`, multiHop), true)
			defer finish()
			startEmptyRequest(t, c, info)
			assert.False(t, c.GetBool("request_plugin_stream_payload_sent"), "empty early events leave host retries available")
			require.True(t, requestplugin.StreamStartFailed(c, types.NewErrorWithStatusCode(errors.New("upstream unavailable"), types.ErrorCodeBadResponse, 502)))
			events := startEvents(rec.snapshot())
			failureIndex := 2
			wantStatus := "incomplete"
			if multiHop {
				failureIndex = 3
				wantStatus = "completed"
			}
			require.Len(t, events, failureIndex+1)
			if multiHop {
				assert.Equal(t, "response.output_item.done", events[2].Get("type").Str)
			}
			assert.Equal(t, "response.failed", events[failureIndex].Get("type").Str)
			assert.Equal(t, wantStatus, events[failureIndex].Get("response.output.0.status").Str)
		})
	}
}

func TestStreamStartUsesClientProtocolAfterConversion(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, upstream := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
			t.Run(fmt.Sprintf("responses=%t/upstream=%s", responses, upstream), func(t *testing.T) {
				c, info, rec, finish := startFixture(t, "before_upstream", responses)
				defer finish()
				info.FinalRequestRelayFormat = upstream
				body := `{"stream":true,"messages":[]}`
				if upstream == types.RelayFormatGemini {
					body = `{"contents":[]}`
				}
				require.Nil(t, requestplugin.Egress(c, info, []byte(body)))
				require.NotNil(t, info.RequestPluginStreamStart)
				helper.SetEventStreamHeaders(c)
				startEmptyRequest(t, c, info)
				events := startEvents(rec.Body.String())
				require.Len(t, events, 1)
				if responses {
					assert.Equal(t, "response.created", events[0].Get("type").Str)
				} else {
					assert.Equal(t, "assistant", events[0].Get("choices.0.delta.role").Str)
				}
			})
		}
	}
}

func TestStreamStartRetryAndFailures(t *testing.T) {
	for _, timing := range []string{"before_upstream", "after_connect"} {
		t.Run(timing, func(t *testing.T) {
			c, info, rec, finish := startFixture(t, timing, true)
			service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })
			_, err := startRequest(t, c, info)
			require.Error(t, err)
			if timing == "before_upstream" {
				assert.Len(t, startEvents(rec.Body.String()), 1)
			} else {
				assert.Empty(t, rec.Body.String())
			}
			// An early start alone does not suppress a valid upstream retry.
			assert.True(t, service.ShouldRetryRelayError(c, types.NewErrorWithStatusCode(errors.New("retry"), types.ErrorCodeBadResponse, 502), 1))
			finish()
			info.RetryIndex++
			finish = requestplugin.Reply(c, info.RelayFormat, info)
			defer finish()
			info.InitChannelMeta(c)
			require.Nil(t, requestplugin.Egress(c, info, []byte(`{"stream":true}`)))
			service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			resp, err := startRequest(t, c, info)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Len(t, startEvents(rec.Body.String()), 1)
			require.True(t, requestplugin.StreamStartFailed(c, types.NewErrorWithStatusCode(errors.New("final failure"), types.ErrorCodeBadResponse, 502)))
			events := startEvents(rec.Body.String())
			require.Len(t, events, 2)
			assert.Equal(t, "response.failed", events[1].Get("type").Str)
			assert.Equal(t, events[0].Get("response.id").Str, events[1].Get("response.id").Str)
			assert.Equal(t, int64(1), events[1].Get("sequence_number").Int())
		})
	}
}

func TestStreamStartExclusionsAndEffectValidation(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"stream.start","timing":"now"}]`, `[{"type":"stream.start"}]`, `[{"type":"stream.start","timing":"after_connect","text":"hello"}]`,
		`[{"type":"stream.start","timing":"before_upstream","delayMs":-1}]`, `[{"type":"stream.start","timing":"before_upstream","delayMs":60001}]`,
		`[{"type":"stream.start","timing":"before_upstream","delayMs":1.5}]`, `[{"type":"stream.start","timing":"before_upstream","delayMs":null}]`,
		`[{"type":"stream.start","timing":"before_upstream","delayMs":"1000"}]`, `[{"type":"stream.start","timing":"after_connect","delayMs":0}]`,
		`[{"type":"stream.start","timing":"after_connect","outputItem":null}]`, `[{"type":"stream.start","timing":"after_connect","outputItem":"true"}]`,
		`[{"type":"stream.start","timing":"after_connect","outputItem":true,"completeOutputItem":null}]`,
		`[{"type":"stream.start","timing":"after_connect","outputItem":true,"completeOutputItem":"true"}]`,
		`[{"type":"stream.start","timing":"after_connect","completeOutputItem":true}]`,
		`[{"type":"stream.start","timing":"after_connect","outputItem":false,"completeOutputItem":true}]`,
	} {
		_, err := requestpatch.DecodeTransformEffects([]byte(raw), requestpatch.EffectStreamStart)
		require.Error(t, err)
	}
	_, err := requestpatch.DecodeEffects([]byte(`[{"type":"stream.start","timing":"after_connect"}]`))
	require.Error(t, err, "provider plugins cannot emit client events")
	for _, variant := range []string{"nonstream", "upstream rejection", "nonstream upstream", "canceled", "egress rejected", "non-http", "task"} {
		t.Run(variant, func(t *testing.T) {
			c, info, rec, finish := startFixture(t, "after_connect", false)
			defer finish()
			switch variant {
			case "task":
				body := `{"stream":true,"prompt":"image"}`
				reader, apiErr := requestplugin.TaskEgress(c, info, strings.NewReader(body))
				require.Nil(t, apiErr)
				raw, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, body, string(raw))
				assert.Empty(t, rec.Body.String())
				return
			case "nonstream":
				info.IsStream = false
				require.Nil(t, requestplugin.Egress(c, info, []byte(`{"stream":false}`)))
			case "canceled":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			case "non-http":
				info.ClientWs = &websocket.Conn{}
				require.Nil(t, requestplugin.Egress(c, info, []byte(`{"stream":true}`)))
			case "egress rejected":
				plugin, err := jsplugin.DefaultRegistry.Register(`export const meta={apiVersion:1,key:"reject-start",name:"Reject",version:"1.0.0",author:{name:"Test"}}; export function egress(){return {action:"reject",code:"blocked"}}`, jsplugin.Options{})
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key)) })
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: `[{"id":"a","name":"start","plugin":"first-token-optimization","enabled":true,"global":true},{"id":"b","name":"stop","plugin":"reject-start","enabled":true,"global":true}]`}))
				_, _, ingressErr := requestplugin.Ingress(c, info.RelayFormat)
				require.Nil(t, ingressErr)
				require.NotNil(t, requestplugin.Egress(c, info, []byte(`{"stream":true}`)))
				assert.Empty(t, rec.Body.String())
				return
			}
			status := 200
			if variant == "upstream rejection" {
				status = 429
			}
			service.GetHttpClient().Transport = startTransport(func(req *http.Request) (*http.Response, error) {
				header := http.Header{}
				if variant == "nonstream upstream" {
					header.Set("Content-Type", "application/json")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			})
			resp, _ := startRequest(t, c, info)
			if resp != nil {
				require.NoError(t, resp.Body.Close())
			}
			assert.Empty(t, rec.Body.String())
		})
	}
}

func TestStreamStartChatKeepsContentToolsAndUsage(t *testing.T) {
	c, info, rec, finish := startFixture(t, "before_upstream", false)
	defer finish()
	helper.SetEventStreamHeaders(c)
	startEmptyRequest(t, c, info)
	frames := "data: {\"id\":\"real\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"real\",\"created\":2,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"}]}}]}\n\n" +
		"data: {\"id\":\"real\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n"
	for _, chunk := range []string{frames[:8], frames[8:49], frames[49:]} {
		n, err := c.Writer.WriteString(chunk)
		require.NoError(t, err)
		assert.Equal(t, len(chunk), n)
	}
	c.Writer.Flush()
	require.Nil(t, requestplugin.FinishStreamStart(c))
	events := startEvents(rec.Body.String())
	require.Len(t, events, 3)
	assert.Equal(t, "hello", events[1].Get("choices.0.delta.content").Str)
	assert.False(t, events[1].Get("choices.0.delta.role").Exists())
	assert.Equal(t, "call_1", events[1].Get("choices.0.delta.tool_calls.0.id").Str)
	assert.Equal(t, events[0].Get("id").Str, events[1].Get("id").Str)
	assert.Equal(t, int64(100), events[2].Get("usage.prompt_tokens").Int())
	assert.True(t, strings.HasSuffix(rec.Body.String(), "data: [DONE]\n\n"))
}

func TestStreamStartAliasExpiryAndStorageFailure(t *testing.T) {
	c, info, rec, finish := startFixture(t, "before_upstream", true)
	defer finish()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = oldClient, oldEnabled; require.NoError(t, client.Close()) })
	helper.SetEventStreamHeaders(c)
	startEmptyRequest(t, c, info)
	require.NoError(t, helper.StringData(c, `{"type":"response.created","response":{"id":"resp_storage"}}`))
	id := startEvents(rec.Body.String())[0].Get("response.id").Str
	keys := server.Keys()
	require.Len(t, keys, 1)
	stored, err := server.Get(keys[0])
	require.NoError(t, err)
	assert.NotContains(t, stored, info.ApiKey)
	assert.Equal(t, 30*24*time.Hour, server.TTL(keys[0]))
	server.FastForward(31 * 24 * time.Hour)
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"previous_response_id":%q}`, id)))
	request.Set("id", 101)
	request.Set("token_id", 202)
	_, aliasErr := service.ResolveResponseAlias(request, "gpt-test")
	require.NotNil(t, aliasErr)
	assert.Equal(t, 400, aliasErr.StatusCode)
	server.SetError("ERR storage unavailable")
	_, aliasErr = service.ResolveResponseAlias(request, "gpt-test")
	require.NotNil(t, aliasErr)
	assert.Equal(t, 503, aliasErr.StatusCode)
	assert.Error(t, helper.StringData(c, `{"type":"response.completed","response":{"id":"resp_storage","status":"completed"}}`))
	failure := requestplugin.FinishStreamStart(c)
	require.NotNil(t, failure)
	require.True(t, requestplugin.StreamStartFailed(c, failure))
	events := startEvents(rec.Body.String())
	require.Len(t, events, 2)
	assert.Equal(t, "response.failed", events[1].Get("type").Str)
	common.CleanupBodyStorage(request)
}

func TestStreamStartComposesWithReplyReportAndKeepAlive(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err == nil {
			received <- string(raw)
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(server.Close)
	plugin, err := jsplugin.DefaultRegistry.Register(`export const meta={apiVersion:1,key:"start-observer",name:"Observer",version:"1.0.0",author:{name:"Test"},configSchema:{type:"object",properties:{url:{type:"string",title:"URL",format:"url"}}}};
export function reply(ctx,res){return {action:"continue", mask:res.outputs.filter(o=>o.text.length).map(o=>({index:o.index,start:0,end:o.text.length}))};}
export function report(ctx,record){utils.fetch({url:ctx.config.url,method:"POST",body:JSON.stringify(record)});}`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key)) })
	c, info, rec, finish := startFixture(t, "before_upstream", false,
		`{"id":"duplicate","name":"Other start","plugin":"first-token-optimization","enabled":true,"global":true,"config":{"timing":"before_upstream","delayMinSeconds":0,"delayMaxSeconds":0}}`,
		fmt.Sprintf(`{"id":"observer","name":"Observer","plugin":"start-observer","enabled":true,"global":true,"config":{"url":%q}}`, server.URL))
	stopKeepAlive := helper.StartStreamKeepAlive(c, time.Hour)
	t.Cleanup(stopKeepAlive)
	startEmptyRequest(t, c, info)
	assert.Len(t, startEvents(rec.Body.String()), 1, "duplicate instances emit one start")
	require.NoError(t, helper.StringData(c, `{"id":"upstream","choices":[{"index":0,"delta":{"content":"secret"},"finish_reason":null}]}`))
	require.NoError(t, helper.StringData(c, `{"id":"upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`))
	helper.Done(c)
	finish()
	require.Nil(t, requestplugin.FinishStreamStart(c))
	assert.NotContains(t, rec.Body.String(), "secret")
	assert.Contains(t, rec.Body.String(), "***")
	requestplugin.Report(c, info.RelayFormat, info, nil, nil)
	select {
	case raw := <-received:
		record := gjson.Parse(raw)
		assert.Equal(t, int64(200), record.Get("response.status").Int())
		events := record.Get("response.events").Array()
		require.Len(t, events, len(startEvents(rec.Body.String())))
		assert.Equal(t, startEvents(rec.Body.String())[0].Get("id").Str, events[0].Get("data.id").Str)
		assert.NotContains(t, record.Get("response").Raw, "secret")
	case <-time.After(10 * time.Second):
		t.Fatal("report was not delivered")
	}
}
