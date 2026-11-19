/** Schedule a native protocol start event without generating assistant content. */
export const meta = {
  apiVersion: 1,
  key: "first-token-optimization",
  name: { en: "First token optimization", zh: "首字优化" },
  description: {
    en: "Send a native start event early for streaming Chat Completions and Responses",
    zh: "为流式 Chat Completions 和 Responses 提前发送协议开始事件",
  },
  version: "1.0.0",
  author: { name: "QuantumNous" },
  configSchema: {
    type: "object",
    properties: {
      timing: {
        type: "string",
        title: { en: "Start event timing", zh: "开始事件发送时机" },
        description: {
          en: "After connection means after receiving successful upstream HTTP headers",
          zh: "连接成功指已收到上游成功的 HTTP 响应头",
        },
        enum: ["before_upstream", "after_connect"],
        enumTitles: [
          { en: "Before requesting upstream", zh: "请求上游前" },
          { en: "After upstream connection succeeds", zh: "上游连接建立成功后" },
        ],
        default: "after_connect",
      },
    },
  },
};

/** Queue one host-owned start event; the host checks client protocol and transport. */
export function egress(ctx, req) {
  if (
    !req.body ||
    req.body.stream === false ||
    ctx.format === "task" ||
    !["/v1/chat/completions", "/v1/responses", "/pg/chat/completions"].includes(ctx.path)
  ) {
    return { action: "continue" };
  }
  // ctx.format describes the converted upstream body. Gemini has no stream
  // member; the host checks whether the client uses a supported HTTP stream.
  return { action: "continue", effects: [{ type: "stream.start", timing: ctx.config.timing || "after_connect" }] };
}
