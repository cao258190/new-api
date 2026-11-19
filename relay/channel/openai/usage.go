package openai

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type responseUsageField struct {
	path  string
	value any
}

// RewriteResponseUsageJSON rewrites a client-visible usage snapshot without
// changing a protocol converter's original cumulative accounting state.
func RewriteResponseUsageJSON(info *relaycommon.RelayInfo, payload []byte, format types.RelayFormat) ([]byte, error) {
	if info == nil || info.ResponseUsageRewrite == nil {
		return payload, nil
	}
	path := "usage"
	if format == types.RelayFormatOpenAIResponses && ojson.GetBytes(payload, "response").IsObject() {
		path = "response.usage"
	} else if format == types.RelayFormatClaude && ojson.GetBytes(payload, "message").IsObject() {
		path = "message.usage"
	} else if format == types.RelayFormatGemini {
		path = "usageMetadata"
	}
	raw := ojson.GetBytes(payload, path)
	if !raw.IsObject() {
		return payload, nil
	}
	var usage *dto.Usage
	switch format {
	case types.RelayFormatClaude:
		var upstream dto.ClaudeUsage
		if err := common.UnmarshalJsonStr(raw.Raw, &upstream); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromClaudeAPIUsage(&upstream)
	case types.RelayFormatGemini:
		var metadata dto.GeminiUsageMetadata
		if err := common.UnmarshalJsonStr(raw.Raw, &metadata); err != nil {
			return nil, err
		}
		usage = relayconvert.UsageFromGeminiMetadata(&metadata, 0)
	default:
		usage = &dto.Usage{}
		if err := common.UnmarshalJsonStr(raw.Raw, usage); err != nil {
			return nil, err
		}
		if format == types.RelayFormatOpenAIResponses {
			usage = relayconvert.NormalizeResponsesUsage(usage)
		} else {
			applyUsagePostProcessing(info, usage, payload)
		}
	}
	rewritten := info.RewriteResponseUsage(usage)
	if rewritten == usage {
		return payload, nil
	}
	return PatchResponseUsageJSON(payload, path, format, rewritten)
}

// PatchResponseUsageJSON writes checked accounting facts in a client's
// protocol, retaining unrelated upstream fields and the canonical billing
// snapshot. It also writes explicit zeros so old cache aliases cannot win.
func PatchResponseUsageJSON(payload []byte, path string, format types.RelayFormat, usage *dto.Usage) ([]byte, error) {
	if usage == nil {
		return payload, nil
	}
	canonical := usage
	if normalized, ok := usage.BillingUsage.CanonicalUsage(); ok {
		canonical = normalized
	}
	read := canonical.PromptTokensDetails.CachedTokens
	write := canonical.PromptTokensDetails.CacheCreationTokensTotal()
	input, output := canonical.PromptTokens, canonical.CompletionTokens
	if strings.EqualFold(canonical.UsageSemantic, dto.BillingUsageSemanticAnthropic) {
		input += read + write
	}
	oneHour := canonical.ClaudeCacheCreation1hTokens
	twelveHours := canonical.PromptTokensDetails.CacheWrite12hTokens()
	var fields []responseUsageField
	switch format {
	case types.RelayFormatClaude:
		fields = append(fields, responseUsageField{"input_tokens", max(input-read-write, 0)},
			responseUsageField{"output_tokens", output},
			responseUsageField{"cache_read_input_tokens", read},
			responseUsageField{"cache_creation_input_tokens", write},
			responseUsageField{"cache_creation.ephemeral_5m_input_tokens", max(write-oneHour, 0)},
			responseUsageField{"cache_creation.ephemeral_1h_input_tokens", oneHour})
	case types.RelayFormatGemini:
		toolInput := int(min(max(ojson.GetBytes(payload, path+".toolUsePromptTokenCount").Int(), 0), int64(input)))
		reasoning := min(max(canonical.CompletionTokenDetails.ReasoningTokens, 0), output)
		fields = append(fields, responseUsageField{"promptTokenCount", input - toolInput},
			responseUsageField{"toolUsePromptTokenCount", toolInput},
			responseUsageField{"candidatesTokenCount", output - reasoning},
			responseUsageField{"thoughtsTokenCount", reasoning},
			responseUsageField{"totalTokenCount", input + output},
			responseUsageField{"cachedContentTokenCount", read},
			responseUsageField{"cacheWriteTokenCount", write},
			responseUsageField{"cacheWrite1hTokenCount", oneHour},
			responseUsageField{"cacheWrite12hTokenCount", twelveHours})
		for _, details := range []responseUsageField{{"promptTokensDetails", input - toolInput}, {"toolUsePromptTokensDetails", toolInput},
			{"candidatesTokensDetails", output - reasoning}, {"cacheTokensDetails", read}} {
			// Modality details are quantities too. Keep existing TEXT entries
			// consistent while retaining every unrelated provider field.
			textCount := details.value.(int)
			for index, detail := range ojson.GetBytes(payload, path+"."+details.path).Array() {
				if !strings.EqualFold(detail.Get("modality").Str, "TEXT") {
					continue
				}
				fields = append(fields, responseUsageField{fmt.Sprintf("%s.%d.tokenCount", details.path, index), textCount})
				textCount = 0
			}
		}
	default:
		fields = append(fields, responseUsageField{"total_tokens", input + output})
		for _, field := range []responseUsageField{
			{"prompt_tokens", input}, {"completion_tokens", output}, {"input_tokens", input}, {"output_tokens", output},
			{"cached_tokens", read}, {"prompt_cache_hit_tokens", read}, {"prompt_cache_miss_tokens", max(input-read-write, 0)},
			{"claude_cache_creation_5_m_tokens", max(write-oneHour, 0)}, {"claude_cache_creation_1_h_tokens", oneHour},
		} {
			required := format == types.RelayFormatOpenAI && (field.path == "prompt_tokens" || field.path == "completion_tokens") ||
				format == types.RelayFormatOpenAIResponses && (field.path == "input_tokens" || field.path == "output_tokens")
			if field.path == "claude_cache_creation_1_h_tokens" && oneHour > 0 {
				required = true
			}
			if required || ojson.GetBytes(payload, path+"."+field.path).Exists() {
				fields = append(fields, field)
			}
		}
		for _, details := range []string{"prompt_tokens_details", "input_tokens_details"} {
			required := format == types.RelayFormatOpenAI && details == "prompt_tokens_details" ||
				format == types.RelayFormatOpenAIResponses && details == "input_tokens_details"
			if !required && !ojson.GetBytes(payload, path+"."+details).IsObject() {
				continue
			}
			for _, field := range []responseUsageField{
				{"cached_tokens", read}, {"cache_write_tokens", write}, {"cached_creation_tokens", write}, {"cache_creation_input_tokens", write},
				{"cache_write_tokens_details.ttl_30m_tokens", max(write-twelveHours, 0)}, {"cache_write_tokens_details.ttl_12h_tokens", twelveHours},
			} {
				field.path = details + "." + field.path
				fields = append(fields, field)
			}
			for _, field := range []responseUsageField{{"text_tokens", input}, {"cached_tokens_details.text_tokens", read}} {
				if ojson.GetBytes(payload, path+"."+details+"."+field.path).Exists() {
					field.path = details + "." + field.path
					fields = append(fields, field)
				}
			}
		}
		for _, details := range []string{"completion_tokens_details", "output_tokens_details"} {
			if ojson.GetBytes(payload, path+"."+details).IsObject() {
				fields = append(fields, responseUsageField{details + ".reasoning_tokens", canonical.CompletionTokenDetails.ReasoningTokens},
					responseUsageField{details + ".text_tokens", canonical.CompletionTokenDetails.TextTokens})
			}
		}
	}
	fields = append(fields, responseUsageField{"billing_usage", usage.BillingUsage})
	for _, field := range fields {
		encoded, err := common.Marshal(field.value)
		if err != nil {
			return nil, fmt.Errorf("encode response usage %s: %w", field.path, err)
		}
		payload, err = ojson.SetRawBytes(payload, path+"."+field.path, encoded)
		if err != nil {
			return nil, fmt.Errorf("write response usage %s: %w", field.path, err)
		}
	}
	return payload, nil
}

func applyUsagePostProcessing(info *relaycommon.RelayInfo, usage *dto.Usage, responseBody []byte) {
	if info == nil || usage == nil {
		return
	}

	switch info.ChannelType {
	case constant.ChannelTypeDeepSeek:
		if usage.PromptTokensDetails.CachedTokens == 0 && usage.PromptCacheHitTokens != 0 {
			usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
		}
	case constant.ChannelTypeZhipu_v4:
		// 智普的cached_tokens在标准位置: usage.prompt_tokens_details.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeMoonshot:
		// Moonshot的cached_tokens在非标准位置: choices[].usage.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractMoonshotCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeOpenAI:
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if cachedTokens, ok := extractLlamaCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			}
		}
	}
}

func extractCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CachedTokens         *int `json:"cached_tokens"`
			PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Usage.PromptTokensDetails.CachedTokens != nil {
		return *payload.Usage.PromptTokensDetails.CachedTokens, true
	}
	if payload.Usage.CachedTokens != nil {
		return *payload.Usage.CachedTokens, true
	}
	if payload.Usage.PromptCacheHitTokens != nil {
		return *payload.Usage.PromptCacheHitTokens, true
	}
	return 0, false
}

// extractMoonshotCachedTokensFromBody 从Moonshot的非标准位置提取cached_tokens
// Moonshot的流式响应格式: {"choices":[{"usage":{"cached_tokens":111}}]}
func extractMoonshotCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Choices []struct {
			Usage struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"usage"`
		} `json:"choices"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	// 遍历choices查找cached_tokens
	for _, choice := range payload.Choices {
		if choice.Usage.CachedTokens != nil && *choice.Usage.CachedTokens > 0 {
			return *choice.Usage.CachedTokens, true
		}
	}

	return 0, false
}

// extractLlamaCachedTokensFromBody 从llama.cpp的非标准位置提取cache_n
func extractLlamaCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Timings struct {
			CachedTokens *int `json:"cache_n"`
		} `json:"timings"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Timings.CachedTokens == nil {
		return 0, false
	}
	return *payload.Timings.CachedTokens, true
}
