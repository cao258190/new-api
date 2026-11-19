package requestplugin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/gin-gonic/gin"
)

// usageInput identifies a cumulative input snapshot independently of output.
// A stream can publish input before its output count is known.
type usageInput struct {
	Tokens, Read, Write, Write1h, Write12h int
}

func inputOfUsage(usage jsplugin.TokenUsage) usageInput {
	return usageInput{usage.InputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.CacheWrite1hTokens, usage.CacheWrite12hTokens}
}

func (input usageInput) apply(usage jsplugin.TokenUsage) jsplugin.TokenUsage {
	usage.InputTokens, usage.CacheReadTokens, usage.CacheWriteTokens = input.Tokens, input.Read, input.Write
	usage.CacheWrite1hTokens, usage.CacheWrite12hTokens = input.Write1h, input.Write12h
	return usage
}

type usageTarget struct {
	plugin      *jsplugin.LoadedPlugin
	instance    *request_plugin_setting.Instance
	inputs      map[usageInput]usageInput
	disabled    bool
	step        Step
	record      relaycommon.RequestPluginRecord
	stepIndex   int
	recordIndex int
}

// usageCheck belongs to one channel attempt. Its frozen plugins and config are
// those of the request plan; it never writes into a provider's accumulator.
type usageCheck struct {
	ctx       context.Context
	call      Call
	targets   []*usageTarget
	lastInput jsplugin.TokenUsage
	lastValue jsplugin.TokenUsage
	hasLast   bool
}

func newUsageCheck(ctx context.Context, plan *Plan, call Call) *usageCheck {
	check := &usageCheck{ctx: ctx, call: call}
	for _, instance := range plan.scope(jsplugin.UsageHook, call.Group, call.Model, call.Bound) {
		plugin := plan.plugins[instance.Plugin]
		check.targets = append(check.targets, &usageTarget{
			plugin: plugin, instance: instance, inputs: map[usageInput]usageInput{}, stepIndex: -1, recordIndex: -1,
			step: Step{RequestPluginStep: relaycommon.RequestPluginStep{
				Instance: instance.ID, Name: instance.Name, Plugin: instance.Plugin, Version: plugin.Meta.Version, Stage: jsplugin.UsageHook,
			}},
			record: relaycommon.RequestPluginRecord{Plugin: instance.Plugin, Name: pluginName(plugin.Meta), Stage: jsplugin.UsageHook},
		})
	}
	return check
}

// attachUsage installs only the response token policy of supported HTTP text
// endpoints. Image, audio, task and WebSocket accounting keep their own paths.
func attachUsage(c *gin.Context, plan *Plan, format types.RelayFormat, info *relaycommon.RelayInfo) {
	if !replyEndpoint(format, info.RelayMode, c.Request.URL.Path) {
		return
	}
	call := attemptCall(c, info)
	call.Format = string(format)
	check := newUsageCheck(c.Request.Context(), plan, call)
	if len(check.targets) == 0 {
		return
	}
	var lastRewritten *dto.Usage
	frozen := false
	info.ResponseUsageRewrite = func(raw *dto.Usage) *dto.Usage {
		if frozen {
			return lastRewritten
		}
		canonical := *raw
		if original, ok := raw.BillingUsage.CanonicalUsage(); ok {
			canonical = *original
		}
		if canonical.UsageSemantic == "" {
			canonical.UsageSemantic = dto.BillingUsageSemanticOpenAI
			if info.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
				canonical.UsageSemantic = dto.BillingUsageSemanticAnthropic
			}
		}
		before, err := normalizedResponseUsage(&canonical)
		// The OpenRouter legacy cost-derived path does not use ordinary Claude
		// input semantics. Keep that provider's own accounting unchanged.
		unsupported := info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter &&
			canonical.UsageSemantic == dto.BillingUsageSemanticAnthropic
		if err != nil || unsupported {
			for _, target := range check.targets {
				target.step.Action, target.record.Unchecked = "unchecked", true
				if err != nil {
					target.step.Error = errorGuard
				}
			}
			check.publish(info)
			if lastRewritten != nil {
				// Later provider facts cannot revoke input already published by a
				// policy. Freeze at the last fully checked accounting snapshot.
				frozen = true
				info.ResponseUsageAudit["fallback"] = "last_valid_snapshot"
				return lastRewritten
			}
			info.ResponseUsageOriginal, info.ResponseUsageAudit = nil, nil
			info.ResponseUsageInputTokens = 0
			return raw
		}
		after := check.run(before, canonical.UsageSemantic)
		check.publish(info)
		original := canonical
		info.ResponseUsageOriginal = &original
		info.ResponseUsageInputTokens = before.InputTokens
		info.ResponseUsageAudit = map[string]any{"before": before, "after": after, "semantic": canonical.UsageSemantic}
		if after == before {
			if lastRewritten != nil {
				lastRewritten = responseUsageWithCounts(&canonical, after)
			}
			return raw
		}
		lastRewritten = responseUsageWithCounts(&canonical, after)
		return lastRewritten
	}
}

// run applies arithmetic policies in scope order. Failure keeps the previously
// valid quantities, disables that instance for this attempt, and never causes a
// second upstream request. Published input cannot depend on later output.
func (check *usageCheck) run(before jsplugin.TokenUsage, semantic string) jsplugin.TokenUsage {
	if check.hasLast && check.lastInput == before {
		return check.lastValue
	}
	current := before
	for _, target := range check.targets {
		input := inputOfUsage(current)
		committed, published := target.inputs[input]
		if target.disabled {
			if published {
				current = committed.apply(current)
			}
			continue
		}
		call := check.call
		started := time.Now()
		result, err := target.plugin.CallUsage(check.ctx, jsplugin.TransformContext{
			Stage: jsplugin.UsageHook, Format: call.Format, Path: call.Path, Model: call.Model,
			UpstreamModel: call.UpstreamModel, Group: call.Group, Channel: call.Channel, Attempt: call.Attempt,
			User: call.User, Token: call.Token, Instance: jsplugin.TransformInstance{ID: target.instance.ID, Name: target.instance.Name},
			Config: target.instance.Config,
		}, current)
		target.step.DurationMS += time.Since(started).Milliseconds()
		if err == nil && semantic == dto.BillingUsageSemanticAnthropic && result.Usage.CacheWrite12hTokens != 0 {
			err = fmt.Errorf("Claude usage does not support 12h cache writes")
		}
		if err == nil && semantic != "" && semantic != dto.BillingUsageSemanticAnthropic && result.Usage.CacheWrite1hTokens != 0 {
			err = fmt.Errorf("this usage semantic does not support 1h cache writes")
		}
		if err == nil && published && inputOfUsage(result.Usage) != committed {
			err = fmt.Errorf("input usage must not change with the output of the same input snapshot")
		}
		if err == nil && !published && len(target.inputs) >= 16 {
			err = fmt.Errorf("too many distinct input usage snapshots")
		}
		if err != nil {
			target.disabled = true
			target.step.Action, target.step.Error = "fail", errorGuard
			var transformErr *jsplugin.TransformError
			if errors.As(err, &transformErr) {
				target.step.Error = transformErr.Class
			}
			target.step.Detail = target.step.Error + ": " + err.Error()
			if transformErr != nil && transformErr.Thrown != "" {
				target.step.Detail += ": " + transformErr.Thrown
			}
			logger.LogWarn(check.ctx, common.LogText("request plugin instance %s (%s) failed at %s: %s", target.instance.ID, target.instance.Plugin, jsplugin.UsageHook, target.step.Error))
			if published {
				current = committed.apply(current)
			}
			continue
		}
		// Keep every published input mapping for this attempt. Stop applying an
		// exceptional stream's policy at the bound instead of forgetting a mapping.
		target.inputs[input] = inputOfUsage(result.Usage)
		target.step.Action, target.step.Error = "continue", ""
		target.step.Findings = nil
		target.record.Findings, target.record.Notice = nil, result.Notice
		for _, finding := range result.Findings {
			target.step.Findings = append(target.step.Findings, relaycommon.RequestPluginFinding{Rule: finding.Rule, Category: finding.Category, Count: finding.Count})
			target.record.Findings = append(target.record.Findings, relaycommon.RequestPluginCount{Category: finding.Category, Count: finding.Count})
		}
		target.record.Changes = usageChanges(current, result.Usage)
		target.step.Effects = len(target.record.Changes)
		current = result.Usage
	}
	check.lastInput, check.lastValue, check.hasLast = before, current, true
	return current
}

// publish updates the bounded per-instance record instead of appending one
// audit entry for every cumulative usage event in a stream.
func (check *usageCheck) publish(info *relaycommon.RelayInfo) {
	for _, target := range check.targets {
		if target.stepIndex < 0 {
			target.stepIndex = len(info.RequestPluginSteps)
			info.RequestPluginSteps = append(info.RequestPluginSteps, target.step.RequestPluginStep)
		} else {
			info.RequestPluginSteps[target.stepIndex] = target.step.RequestPluginStep
		}
		if target.recordIndex >= 0 {
			info.RequestPluginRecord[target.recordIndex] = target.record
		} else if recorded(target.record) {
			target.recordIndex = len(info.RequestPluginRecord)
			info.RequestPluginRecord = append(info.RequestPluginRecord, target.record)
		}
	}
}

func usageChanges(before, after jsplugin.TokenUsage) []relaycommon.RequestPluginChange {
	var changes []relaycommon.RequestPluginChange
	for _, field := range []struct {
		name          string
		before, after int
	}{
		{"inputTokens", before.InputTokens, after.InputTokens}, {"outputTokens", before.OutputTokens, after.OutputTokens},
		{"cacheReadTokens", before.CacheReadTokens, after.CacheReadTokens}, {"cacheWriteTokens", before.CacheWriteTokens, after.CacheWriteTokens},
		{"cacheWrite1hTokens", before.CacheWrite1hTokens, after.CacheWrite1hTokens}, {"cacheWrite12hTokens", before.CacheWrite12hTokens, after.CacheWrite12hTokens},
	} {
		if field.before != field.after {
			value, _ := common.Marshal(field.after)
			changes = append(changes, relaycommon.RequestPluginChange{Op: "replace", Path: "usage." + field.name, Value: value})
		}
	}
	return changes
}

// normalizedResponseUsage reads the same provider snapshot settlement reads.
// Multimodal detail cannot be safely reassigned with a text-only token policy.
func normalizedResponseUsage(usage *dto.Usage) (jsplugin.TokenUsage, error) {
	for _, input := range []*dto.InputTokenDetails{&usage.PromptTokensDetails, usage.InputTokensDetails} {
		if input == nil {
			continue
		}
		if input.ImageTokens != 0 || input.AudioTokens != 0 {
			return jsplugin.TokenUsage{}, fmt.Errorf("multimodal token usage is not supported")
		}
		if details := input.CachedTokensDetails; details != nil &&
			(details.ImageTokens != nil && *details.ImageTokens != 0 || details.AudioTokens != nil && *details.AudioTokens != 0) {
			return jsplugin.TokenUsage{}, fmt.Errorf("multimodal cached token usage is not supported")
		}
	}
	for _, output := range []*dto.OutputTokenDetails{&usage.CompletionTokenDetails, usage.OutputTokensDetails} {
		if output != nil && (output.ImageTokens != 0 || output.AudioTokens != 0) {
			return jsplugin.TokenUsage{}, fmt.Errorf("multimodal token usage is not supported")
		}
	}
	input := usage.PromptTokens
	if input == 0 && usage.UsageSemantic != dto.BillingUsageSemanticAnthropic {
		input = usage.InputTokens
	}
	write := usage.PromptTokensDetails.CacheCreationTokensTotal()
	read := usage.PromptTokensDetails.CachedTokens
	for _, count := range []int{input, read, write, usage.CompletionTokens} {
		if count < 0 || count > math.MaxInt32 {
			return jsplugin.TokenUsage{}, fmt.Errorf("token usage is outside the supported range")
		}
	}
	if usage.UsageSemantic == dto.BillingUsageSemanticAnthropic {
		total := int64(input) + int64(read) + int64(write)
		if total > math.MaxInt32 {
			return jsplugin.TokenUsage{}, fmt.Errorf("input tokens are outside the supported range")
		}
		input = int(total)
	}
	result := jsplugin.TokenUsage{InputTokens: input, OutputTokens: usage.CompletionTokens, CacheReadTokens: read,
		CacheWriteTokens: write, CacheWrite1hTokens: usage.ClaudeCacheCreation1hTokens,
		CacheWrite12hTokens: usage.PromptTokensDetails.CacheWrite12hTokens()}
	if result.OutputTokens == 0 {
		result.OutputTokens = usage.OutputTokens
	}
	return result, jsplugin.ValidateTokenUsage(result)
}

// responseUsageWithCounts owns its detail pointers and rebuilds the accounting
// snapshot, so an old provider snapshot cannot override the checked counts.
func responseUsageWithCounts(original *dto.Usage, counts jsplugin.TokenUsage) *dto.Usage {
	usage := *original
	usage.PromptTokensDetails = original.PromptTokensDetails.Clone()
	usage.InputTokens, usage.PromptTokens = counts.InputTokens, counts.InputTokens
	usage.OutputTokens, usage.CompletionTokens = counts.OutputTokens, counts.OutputTokens
	usage.PromptCacheHitTokens = counts.CacheReadTokens
	usage.PromptTokensDetails.CachedTokens = counts.CacheReadTokens
	usage.PromptTokensDetails.CachedCreationTokens = counts.CacheWriteTokens
	usage.PromptTokensDetails.CacheCreationInputTokens = counts.CacheWriteTokens
	usage.PromptTokensDetails.CacheWriteTokens = counts.CacheWriteTokens
	usage.PromptTokensDetails.CacheWriteTokensDetails = nil
	usage.ClaudeCacheCreation5mTokens, usage.ClaudeCacheCreation1hTokens = 0, 0
	if usage.UsageSemantic == dto.BillingUsageSemanticAnthropic {
		usage.PromptTokens = counts.InputTokens - counts.CacheReadTokens - counts.CacheWriteTokens
		usage.ClaudeCacheCreation5mTokens = counts.CacheWriteTokens - counts.CacheWrite1hTokens
		usage.ClaudeCacheCreation1hTokens = counts.CacheWrite1hTokens
	} else if counts.CacheWriteTokens > 0 {
		usage.PromptTokensDetails.CacheWriteTokensDetails = &dto.CacheWriteTokenDetails{
			TTL30mTokens: counts.CacheWriteTokens - counts.CacheWrite12hTokens,
			TTL12hTokens: counts.CacheWrite12hTokens,
		}
	}
	if details := usage.PromptTokensDetails.CachedTokensDetails; details != nil && details.TextTokens != nil {
		value := counts.CacheReadTokens
		details.TextTokens = &value
	}
	if usage.PromptTokensDetails.TextTokens > 0 {
		usage.PromptTokensDetails.TextTokens = counts.InputTokens
	}
	if original.InputTokensDetails != nil {
		details := usage.PromptTokensDetails.Clone()
		usage.InputTokensDetails = &details
	}
	usage.CompletionTokenDetails.ReasoningTokens = min(usage.CompletionTokenDetails.ReasoningTokens, counts.OutputTokens)
	if usage.CompletionTokenDetails.TextTokens > 0 {
		usage.CompletionTokenDetails.TextTokens = counts.OutputTokens - usage.CompletionTokenDetails.ReasoningTokens
	}
	if original.OutputTokensDetails != nil {
		details := usage.CompletionTokenDetails
		usage.OutputTokensDetails = &details
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	usage.ResponseUsageRewritten = true
	usage.BillingUsage = dto.NewCanonicalBillingUsage(&usage)
	return &usage
}

// dryRunUsage uses the same checked pipeline without a provider or settlement.
func dryRunUsage(ctx context.Context, plan *Plan, call Call, before jsplugin.TokenUsage, result *DryRunResult) {
	check := newUsageCheck(ctx, plan, call)
	semantic := dto.BillingUsageSemanticOpenAI
	if types.RelayFormat(call.Format) == types.RelayFormatClaude {
		semantic = dto.BillingUsageSemanticAnthropic
	}
	after := check.run(before, semantic)
	result.Usage = &DryRunUsage{Before: before, After: after}
	for _, target := range check.targets {
		result.Steps = append(result.Steps, target.step)
		if recorded(target.record) {
			result.Record = append(result.Record, target.record)
		}
	}
}
