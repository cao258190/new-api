package jsplugin

import (
	"context"
	"fmt"
	"math"
	"slices"
)

// UsageHook rewrites normalized response token quantities before they are
// published or settled. It cannot change the response body or its price.
const UsageHook = "usage"

// TokenUsage is the protocol-independent view given to a usage hook. InputTokens
// includes cache reads and writes; their remainder is uncached input. The 1h and
// 12h counts are disjoint subsets of CacheWriteTokens, not additional tokens.
type TokenUsage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	CacheReadTokens     int `json:"cacheReadTokens"`
	CacheWriteTokens    int `json:"cacheWriteTokens"`
	CacheWrite1hTokens  int `json:"cacheWrite1hTokens"`
	CacheWrite12hTokens int `json:"cacheWrite12hTokens"`
}

// ValidateTokenUsage rejects negative, oversized or inconsistent quantities.
// Every value fits the host's single-request int32 accounting boundary.
func ValidateTokenUsage(usage TokenUsage) error {
	for _, field := range []struct {
		name  string
		value int
	}{
		{"inputTokens", usage.InputTokens},
		{"outputTokens", usage.OutputTokens},
		{"cacheReadTokens", usage.CacheReadTokens},
		{"cacheWriteTokens", usage.CacheWriteTokens},
		{"cacheWrite1hTokens", usage.CacheWrite1hTokens},
		{"cacheWrite12hTokens", usage.CacheWrite12hTokens},
	} {
		if field.value < 0 || field.value > math.MaxInt32 {
			return fmt.Errorf("usage.%s must be an integer from 0 to %d", field.name, math.MaxInt32)
		}
	}
	if int64(usage.CacheReadTokens)+int64(usage.CacheWriteTokens) > int64(usage.InputTokens) {
		return fmt.Errorf("usage.cacheReadTokens plus cacheWriteTokens must not exceed inputTokens")
	}
	if int64(usage.CacheWrite1hTokens)+int64(usage.CacheWrite12hTokens) > int64(usage.CacheWriteTokens) {
		return fmt.Errorf("usage.cacheWrite1hTokens plus cacheWrite12hTokens must not exceed cacheWriteTokens")
	}
	return nil
}

// UsageResult contains the complete quantities after a checked usage patch.
// An omitted patch preserves the input; an explicit zero replaces its value.
type UsageResult struct {
	Action   string
	Usage    TokenUsage
	Changed  bool
	Findings []TransformFinding
	Notice   LocalizedText
}

// CallUsage calls usage(ctx, usage), merges its partial result and validates the
// resulting quantities. No network or request media is available to this hook.
// Errors are TransformErrors and must leave the caller's quantities unchanged.
func (p *LoadedPlugin) CallUsage(ctx context.Context, tctx TransformContext, usage TokenUsage) (UsageResult, error) {
	failure := &TransformError{Plugin: p.Meta.Key, Hook: UsageHook, Class: TransformErrorFailed}
	if !slices.Contains(p.Meta.transformHooks, UsageHook) {
		failure.reason = "the plugin does not export it"
		return UsageResult{}, failure
	}
	if err := ValidateTokenUsage(usage); err != nil {
		failure.Class, failure.reason = TransformErrorResult, err.Error()
		return UsageResult{}, failure
	}
	tctx.Stage, tctx.Media = UsageHook, nil
	tctx.keywordFields = p.Meta.ConfigSchema.KeywordFields()
	callCtx := context.WithValue(WithFetcher(ctx, nil), transformContextKey{}, &tctx)
	value, _, err := p.Engine.call(callCtx, callOptions{}, UsageHook, nil, p.transformHookContext(&tctx), map[string]any{
		"inputTokens": usage.InputTokens, "outputTokens": usage.OutputTokens,
		"cacheReadTokens": usage.CacheReadTokens, "cacheWriteTokens": usage.CacheWriteTokens,
		"cacheWrite1hTokens": usage.CacheWrite1hTokens, "cacheWrite12hTokens": usage.CacheWrite12hTokens,
	})
	if err != nil {
		failure.classify(err)
		return UsageResult{}, failure
	}
	decoded, reason := decodeUsageResult(value, usage)
	if reason != "" {
		failure.Class, failure.reason = TransformErrorResult, reason
		return UsageResult{}, failure
	}
	return decoded, nil
}

// decodeUsageResult accepts only a continuation and a bounded quantity patch.
func decodeUsageResult(value any, usage TokenUsage) (UsageResult, string) {
	object, ok := value.(map[string]any)
	if !ok {
		return UsageResult{}, "the result must be an object"
	}
	if action, _ := object["action"].(string); action != "continue" {
		return UsageResult{}, `action must be "continue"`
	}
	if err := allowMembers(object, "", "action", "usage", "findings", "notice"); err != nil {
		return UsageResult{}, err.Error()
	}
	decoded := UsageResult{Action: "continue", Usage: usage}
	var reason string
	if decoded.Notice, decoded.Findings, reason = decodeNoticeAndFindings(object); reason != "" {
		return UsageResult{}, reason
	}
	if raw, present := object["usage"]; present {
		patch, isObject := raw.(map[string]any)
		if !isObject {
			return UsageResult{}, "usage must be an object"
		}
		if err := allowMembers(patch, "usage", "inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens", "cacheWrite1hTokens", "cacheWrite12hTokens"); err != nil {
			return UsageResult{}, err.Error()
		}
		for _, field := range []struct {
			name   string
			target *int
		}{
			{"inputTokens", &decoded.Usage.InputTokens},
			{"outputTokens", &decoded.Usage.OutputTokens},
			{"cacheReadTokens", &decoded.Usage.CacheReadTokens},
			{"cacheWriteTokens", &decoded.Usage.CacheWriteTokens},
			{"cacheWrite1hTokens", &decoded.Usage.CacheWrite1hTokens},
			{"cacheWrite12hTokens", &decoded.Usage.CacheWrite12hTokens},
		} {
			value, exists := patch[field.name]
			if !exists {
				continue
			}
			count, integer := integerValue(value)
			if !integer || count < 0 || count > math.MaxInt32 {
				return UsageResult{}, fmt.Sprintf("usage.%s must be an integer from 0 to %d", field.name, math.MaxInt32)
			}
			*field.target = int(count)
		}
		if err := ValidateTokenUsage(decoded.Usage); err != nil {
			return UsageResult{}, err.Error()
		}
	}
	decoded.Changed = decoded.Usage != usage
	return decoded, ""
}
