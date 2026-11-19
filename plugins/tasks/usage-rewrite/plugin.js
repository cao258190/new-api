/** Reclassify response token quantities for client reporting and billing. */
export const meta = {
  apiVersion: 1,
  key: "usage-rewrite",
  name: { en: "Token usage rewrite", zh: "Token 用量改写" },
  description: {
    en: "Adjust uncached input and output quantities for reporting and billing",
    zh: "调整未命中缓存的输入和输出用量，用于返回和计费",
  },
  version: "1.0.0",
  author: { name: "QuantumNous" },
  configSchema: {
    type: "object",
    properties: {
      uncachedInputPercent: {
        type: "integer",
        title: { en: "Uncached input retained", zh: "未命中缓存输入保留比例" },
        description: {
          en: "Percentage retained as uncached input; the remainder becomes cache writes",
          zh: "保留为未命中缓存输入的百分比，其余计入缓存写入",
        },
        minimum: 0,
        maximum: 100,
        default: 100,
      },
      cacheWriteTtl: {
        type: "string",
        title: { en: "Cache write lifetime", zh: "缓存写入有效期" },
        description: {
          en: "Lifetime of added cache writes: 1h for Claude accounting, 12h for other accounting",
          zh: "新增缓存写入有效期：Claude 计量支持 1h，其他计量支持 12h",
        },
        enum: ["default", "1h", "12h"],
        enumTitles: [
          { en: "Default", zh: "默认" },
          { en: "One hour", zh: "一小时" },
          { en: "Twelve hours", zh: "十二小时" },
        ],
        default: "default",
      },
      outputPercent: {
        type: "integer",
        title: { en: "Output retained", zh: "输出保留比例" },
        description: { en: "Percentage of output tokens retained", zh: "输出 token 保留的百分比" },
        minimum: 0,
        maximum: 100,
        default: 100,
      },
    },
  },
};

/**
 * Move a proportion of uncached input into cache writes, preserving total input.
 * Existing cache reads and write lifetimes remain intact; rounding stays in the
 * newly written cache category so the input categories always sum to the total.
 */
export function usage(ctx, tokens) {
  const config = ctx.config || {};
  const inputPercent = config.uncachedInputPercent === undefined ? 100 : config.uncachedInputPercent;
  const outputPercent = config.outputPercent === undefined ? 100 : config.outputPercent;
  const uncached = tokens.inputTokens - tokens.cacheReadTokens - tokens.cacheWriteTokens;
  const addedWrites = uncached - Math.floor((uncached * inputPercent) / 100);
  const patch = {
    outputTokens: Math.floor((tokens.outputTokens * outputPercent) / 100),
    cacheWriteTokens: tokens.cacheWriteTokens + addedWrites,
    cacheWrite1hTokens: tokens.cacheWrite1hTokens,
    cacheWrite12hTokens: tokens.cacheWrite12hTokens,
  };
  if (config.cacheWriteTtl === "1h") patch.cacheWrite1hTokens += addedWrites;
  if (config.cacheWriteTtl === "12h") patch.cacheWrite12hTokens += addedWrites;
  return { action: "continue", usage: patch };
}
