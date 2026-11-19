package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseUsageRewriteMatchesAllOpenAIClientProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	chatUsage := `{"prompt_tokens":100,"completion_tokens":2000,"total_tokens":2100,"prompt_tokens_details":{"cached_tokens":89},"vendor_tokens":17}`
	responsesUsage := `{"input_tokens":100,"output_tokens":2000,"total_tokens":2100,"input_tokens_details":{"cached_tokens":89},"vendor_tokens":17}`
	for _, source := range []string{"chat", "responses"} {
		for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", source, format, stream), func(t *testing.T) {
					body := `{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":` + chatUsage + `,"vendor_field":"retained"}`
					if source == "responses" {
						body = `{"id":"resp_1","object":"response","model":"gpt-test","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":` + responsesUsage + `,"vendor_field":"retained"}`
					}
					if stream {
						if source == "chat" {
							body = "data: " + `{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":89}}}` + "\n\n" +
								"data: " + `{"id":"chat_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":` + chatUsage + `}` + "\n\ndata: [DONE]\n\n"
						} else {
							body = "data: " + `{"type":"response.created","response":{"id":"resp_1","model":"gpt-test","status":"in_progress","usage":{"input_tokens":100,"output_tokens":0,"total_tokens":100,"input_tokens_details":{"cached_tokens":89}}}}` + "\n\n" +
								"data: " + `{"type":"response.output_text.delta","delta":"hello"}` + "\n\n" +
								"data: " + `{"type":"response.completed","response":` + body + `}` + "\n\ndata: [DONE]\n\n"
						}
					}
					c, recorder, resp, info := newResponsesChatTestContext(t, body, stream)
					info.RelayFormat = format
					info.SetEstimatePromptTokens(100)
					var rawWrites []int
					info.ResponseUsageRewrite = func(raw *dto.Usage) *dto.Usage {
						original := raw
						if canonical, ok := raw.BillingUsage.CanonicalUsage(); ok {
							original = canonical
						}
						rawWrites = append(rawWrites, original.PromptTokensDetails.CacheCreationTokensTotal())
						adjusted := *original
						adjusted.PromptTokensDetails = original.PromptTokensDetails.Clone()
						adjusted.InputTokens, adjusted.PromptTokens = 100, 100
						adjusted.OutputTokens, adjusted.CompletionTokens = 0, 0
						adjusted.TotalTokens = 100
						adjusted.PromptTokensDetails.CachedTokens = 89
						adjusted.PromptTokensDetails.CacheWriteTokens = original.PromptTokensDetails.CacheCreationTokensTotal() + 7
						adjusted.ResponseUsageRewritten = true
						adjusted.BillingUsage = dto.NewCanonicalBillingUsage(&adjusted)
						return &adjusted
					}
					var usage *dto.Usage
					var apiErr *types.NewAPIError
					if source == "chat" {
						if format == types.RelayFormatOpenAIResponses {
							if stream {
								usage, apiErr = OaiChatToResponsesStreamHandler(c, info, resp)
							} else {
								usage, apiErr = OaiChatToResponsesHandler(c, info, resp)
							}
						} else if stream {
							usage, apiErr = OaiStreamHandler(c, info, resp)
						} else {
							usage, apiErr = OpenaiHandler(c, info, resp)
						}
					} else if format == types.RelayFormatOpenAIResponses {
						if stream {
							usage, apiErr = OaiResponsesStreamHandler(c, info, resp)
						} else {
							usage, apiErr = OaiResponsesHandler(c, info, resp)
						}
					} else if stream {
						usage, apiErr = OaiResponsesToChatStreamHandler(c, info, resp)
					} else {
						usage, apiErr = OaiResponsesToChatHandler(c, info, resp)
					}
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Zero(t, usage.CompletionTokens, "an explicit rewritten zero must not be estimated again")
					assert.Equal(t, 7, usage.PromptTokensDetails.CacheCreationTokensTotal())
					require.NotEmpty(t, rawWrites)
					for _, writes := range rawWrites {
						assert.Zero(t, writes, "the policy must only receive original cumulative snapshots")
					}
					var visible ojson.Result
					for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
						line = strings.TrimPrefix(line, "data: ")
						for _, path := range []string{"usage", "response.usage", "message.usage", "usageMetadata"} {
							if candidate := ojson.Get(line, path); candidate.IsObject() {
								visible = candidate
							}
						}
					}
					require.True(t, visible.IsObject(), recorder.Body.String())
					inputPath, outputPath, readPath, writePath := "prompt_tokens", "completion_tokens", "prompt_tokens_details.cached_tokens", "prompt_tokens_details.cache_write_tokens"
					wantInput := int64(100)
					switch format {
					case types.RelayFormatOpenAIResponses:
						inputPath, outputPath, readPath, writePath = "input_tokens", "output_tokens", "input_tokens_details.cached_tokens", "input_tokens_details.cache_write_tokens"
					case types.RelayFormatClaude:
						inputPath, outputPath, readPath, writePath = "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"
						wantInput = 4
					case types.RelayFormatGemini:
						inputPath, outputPath, readPath, writePath = "promptTokenCount", "candidatesTokenCount", "cachedContentTokenCount", "cacheWriteTokenCount"
					}
					assert.Equal(t, wantInput, visible.Get(inputPath).Int())
					assert.Zero(t, visible.Get(outputPath).Int())
					assert.Equal(t, int64(89), visible.Get(readPath).Int())
					assert.Equal(t, int64(7), visible.Get(writePath).Int())
					var billing dto.BillingUsage
					require.NoError(t, common.UnmarshalJsonStr(visible.Get("billing_usage").Raw, &billing))
					canonical, ok := billing.CanonicalUsage()
					require.True(t, ok)
					assert.Zero(t, canonical.CompletionTokens)
					assert.Equal(t, 7, canonical.PromptTokensDetails.CacheCreationTokensTotal())
					if !stream && (source == "chat" && format == types.RelayFormatOpenAI || source == "responses" && format == types.RelayFormatOpenAIResponses) {
						assert.Equal(t, "retained", ojson.Get(recorder.Body.String(), "vendor_field").Str)
						assert.Equal(t, int64(17), visible.Get("vendor_tokens").Int())
					}
				})
			}
		}
	}
}

func TestResponseUsageRewriteKeepsExistingTextDetailsConsistent(t *testing.T) {
	usage := &dto.Usage{PromptTokens: 60, InputTokens: 60, CompletionTokens: 10, OutputTokens: 10, TotalTokens: 70,
		UsageSemantic: dto.BillingUsageSemanticOpenAI, ResponseUsageRewritten: true,
		PromptTokensDetails:    dto.InputTokenDetails{CachedTokens: 20, CacheWriteTokens: 7, TextTokens: 60},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 3, TextTokens: 7}}
	usage.BillingUsage = dto.NewCanonicalBillingUsage(usage)
	for _, tc := range []struct {
		format     types.RelayFormat
		path, body string
		want       map[string]int64
	}{
		{types.RelayFormatOpenAI, "usage", `{"usage":{"prompt_tokens":100,"completion_tokens":2000,"cached_tokens":89,"prompt_tokens_details":{"text_tokens":100,"cached_tokens_details":{"text_tokens":89},"vendor_field":"retained"},"completion_tokens_details":{"reasoning_tokens":500,"text_tokens":1500}}}`,
			map[string]int64{"prompt_tokens": 60, "completion_tokens": 10, "cached_tokens": 20, "prompt_tokens_details.text_tokens": 60, "prompt_tokens_details.cached_tokens_details.text_tokens": 20, "completion_tokens_details.reasoning_tokens": 3, "completion_tokens_details.text_tokens": 7}},
		{types.RelayFormatOpenAIResponses, "usage", `{"usage":{"input_tokens":100,"output_tokens":2000,"input_tokens_details":{"text_tokens":100,"cached_tokens_details":{"text_tokens":89},"vendor_field":"retained"},"output_tokens_details":{"reasoning_tokens":500,"text_tokens":1500}}}`,
			map[string]int64{"input_tokens": 60, "output_tokens": 10, "input_tokens_details.text_tokens": 60, "input_tokens_details.cached_tokens_details.text_tokens": 20, "output_tokens_details.reasoning_tokens": 3, "output_tokens_details.text_tokens": 7}},
		{types.RelayFormatGemini, "usageMetadata", `{"usageMetadata":{"promptTokenCount":90,"toolUsePromptTokenCount":10,"candidatesTokenCount":1500,"thoughtsTokenCount":500,"promptTokensDetails":[{"modality":"TEXT","tokenCount":90,"vendor_field":"retained"}],"toolUsePromptTokensDetails":[{"modality":"TEXT","tokenCount":10}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":1500}],"cacheTokensDetails":[{"modality":"TEXT","tokenCount":89}]}}`,
			map[string]int64{"promptTokenCount": 50, "toolUsePromptTokenCount": 10, "candidatesTokenCount": 7, "thoughtsTokenCount": 3, "totalTokenCount": 70, "promptTokensDetails.0.tokenCount": 50, "toolUsePromptTokensDetails.0.tokenCount": 10, "candidatesTokensDetails.0.tokenCount": 7, "cacheTokensDetails.0.tokenCount": 20}},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			body, err := PatchResponseUsageJSON([]byte(tc.body), tc.path, tc.format, usage)
			require.NoError(t, err)
			for path, want := range tc.want {
				assert.Equal(t, want, ojson.GetBytes(body, tc.path+"."+path).Int(), path)
			}
			assert.Contains(t, string(body), `"vendor_field":"retained"`)
		})
	}
}

func newResponsesChatTestContext(t *testing.T, body string, isStream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "responses-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		IsStream:           isStream,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	return c, recorder, resp, info
}

func TestOaiResponsesToChatStreamHandlerConvertsSSEOrderAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","created_at":1710000000}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"q\":\"x\"}"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 5, usage.TotalTokens)

	got := recorder.Body.String()
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, got, `"role":"assistant"`)
	require.Contains(t, got, `"content":"hello"`)
	require.Contains(t, got, `"name":"lookup"`)
	require.Contains(t, got, `"arguments":"{\"q\":\"x\"}"`)
	require.Contains(t, got, `"finish_reason":"tool_calls"`)
	require.Contains(t, got, `"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5`)
	require.Contains(t, got, `data: [DONE]`)
	requireOrderedSubstrings(t, got,
		`"role":"assistant"`,
		`"content":"hello"`,
		`"name":"lookup"`,
		`"arguments":"{\"q\":\"x\"}"`,
		`"finish_reason":"tool_calls"`,
		`"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5`,
		`data: [DONE]`,
	)
}

func TestOaiResponsesToChatStreamHandlerConvertsClaudeSSETerminalsAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","created_at":1710000000}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	info.RelayFormat = types.RelayFormatClaude

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 2, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
	assert.Equal(t, 5, usage.TotalTokens)

	got := recorder.Body.String()
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, 1, strings.Count(got, "event: message_start\n"))
	assert.Equal(t, 1, strings.Count(got, "event: content_block_stop\n"))
	assert.Equal(t, 1, strings.Count(got, "event: message_delta\n"))
	assert.Equal(t, 1, strings.Count(got, "event: message_stop\n"))

	messageDeltaFrame := ""
	for frame := range strings.SplitSeq(got, "\n\n") {
		if strings.HasPrefix(frame, "event: message_delta\n") {
			messageDeltaFrame = frame
			break
		}
	}
	require.NotEmpty(t, messageDeltaFrame)
	assert.Contains(t, messageDeltaFrame, `"type":"message_delta"`)
	assert.Contains(t, messageDeltaFrame, `"stop_reason":"end_turn"`)
	assert.Contains(t, messageDeltaFrame, `"input_tokens":2`)
	assert.Contains(t, messageDeltaFrame, `"output_tokens":3`)
	requireOrderedSubstrings(t, got,
		"event: message_start\n",
		"event: content_block_stop\n",
		"event: message_delta\n",
		"event: message_stop\n",
	)
}

func TestOaiResponsesToChatBufferedStreamHandlerReturnsJSONFromSSE(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"buffered text"}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"q\":\"x\"}"}`,
		`data: {"type":"response.done","response":{"model":"gpt-test","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)

	usage, err := OaiResponsesToChatBufferedStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.TotalTokens)

	got := recorder.Body.String()
	require.NotContains(t, got, `data:`)
	require.Contains(t, got, `"object":"chat.completion"`)
	require.Contains(t, got, `"content":"buffered text"`)
	require.Contains(t, got, `"name":"lookup"`)
	require.Contains(t, got, `"arguments":"{\"q\":\"x\"}"`)
	require.Contains(t, got, `"finish_reason":"tool_calls"`)
}

func TestOaiResponsesToChatBufferedStreamHandlerPreservesInterleavedClaudeContent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"rs_1","delta":"**Planning file inspection**"}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","output_index":1,"item_id":"msg_1","delta":"I’ll inspect the starter repository."}`,
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"reasoning","id":"rs_2","summary":[]}}`,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":2,"item_id":"rs_2","delta":"**Clarifying environment task requirements**"}`,
		`data: {"type":"response.output_item.added","output_index":3,"item":{"type":"message","id":"msg_2","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","output_index":3,"item_id":"msg_2","delta":"What would you like me to build?"}`,
		`data: {"type":"response.done","response":{"id":"resp_1","model":"gpt-test","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, false)
	info.RelayFormat = types.RelayFormatClaude

	usage, apiErr := OaiResponsesToChatBufferedStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TotalTokens)

	var claudeResponse dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &claudeResponse))
	require.Len(t, claudeResponse.Content, 4)
	assert.Equal(t, []string{"thinking", "text", "thinking", "text"}, []string{
		claudeResponse.Content[0].Type,
		claudeResponse.Content[1].Type,
		claudeResponse.Content[2].Type,
		claudeResponse.Content[3].Type,
	})
	require.NotNil(t, claudeResponse.Content[0].Thinking)
	require.NotNil(t, claudeResponse.Content[2].Thinking)
	assert.Equal(t, "**Planning file inspection**", *claudeResponse.Content[0].Thinking)
	assert.Equal(t, "I’ll inspect the starter repository.", claudeResponse.Content[1].GetText())
	assert.Equal(t, "**Clarifying environment task requirements**", *claudeResponse.Content[2].Thinking)
	assert.Equal(t, "What would you like me to build?", claudeResponse.Content[3].GetText())
}

func TestOaiChatToResponsesStreamHandlerConvertsSSEOrderAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	usage, err := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 5, usage.TotalTokens)

	got := recorder.Body.String()
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Contains(t, got, `event: response.created`)
	require.Contains(t, got, `event: response.output_text.delta`)
	require.Contains(t, got, `"delta":"hello"`)
	require.Contains(t, got, `event: response.function_call_arguments.delta`)
	require.Contains(t, got, `"delta":"{\"q\":\"x\"}"`)
	require.Contains(t, got, `event: response.completed`)
	require.Contains(t, got, `"input_tokens":2`)
	require.Contains(t, got, `"output_tokens":3`)
	requireOrderedSubstrings(t, got,
		`event: response.created`,
		`event: response.output_item.added`,
		`event: response.output_text.delta`,
		`event: response.output_item.added`,
		`event: response.function_call_arguments.delta`,
		`event: response.output_text.done`,
		`event: response.function_call_arguments.done`,
		`event: response.completed`,
	)
}

func TestOaiChatToResponsesStreamHandlerRestoresCodexCustomToolCall(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_exec","type":"function","function":{"name":"exec","arguments":"{\"input\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls -la\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp, info := newResponsesChatTestContext(t, body, true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	tools, marshalErr := common.Marshal([]map[string]any{
		{"type": "custom", "name": "exec", "description": "Run a shell command", "format": map[string]any{"type": "text"}},
	})
	require.NoError(t, marshalErr)
	converted, convertErr := service.ConvertRequestByID(c, info, relayconvert.ConverterOpenAIResponsesToOpenAIChat, &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: []byte(`"list files"`),
		Tools: tools,
	})
	require.NoError(t, convertErr)
	chatRequest, ok := converted.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatRequest.Tools, 1)
	assert.Equal(t, "function", chatRequest.Tools[0].Type)
	assert.Equal(t, "exec", chatRequest.Tools[0].Function.Name)

	_, err := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)

	got := recorder.Body.String()
	assert.NotContains(t, got, `event: response.function_call_arguments`)
	assert.Contains(t, got, `"delta":"ls -la"`)
	assert.Contains(t, got, `"input":"ls -la"`)
	requireOrderedSubstrings(t, got,
		`event: response.output_item.added`,
		`"type":"custom_tool_call"`,
		`event: response.custom_tool_call_input.delta`,
		`event: response.custom_tool_call_input.done`,
		`event: response.output_item.done`,
		`event: response.completed`,
		`"type":"custom_tool_call"`,
	)
}

func requireOrderedSubstrings(t *testing.T, s string, parts ...string) {
	t.Helper()

	offset := 0
	for _, part := range parts {
		idx := strings.Index(s[offset:], part)
		require.NotEqualf(t, -1, idx, "missing %q after byte offset %d", part, offset)
		offset += idx + len(part)
	}
}
