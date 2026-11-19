/** Schedule a native protocol start event without generating assistant content. */
export const meta = {
  apiVersion: 1,
  key: "first-token-optimization",
  name: { en: "First token optimization", zh: "首字优化" },
  description: {
    en: "Send a native start event early for streaming Chat Completions and Responses",
    zh: "为流式 Chat Completions 和 Responses 提前发送协议开始事件",
  },
  version: "1.1.0",
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
      delayMinSeconds: {
        type: "number",
        title: {
          en: "Minimum start delay",
          zh: "提前包最小延迟",
          "zh-TW": "提前封包最短延遲",
          fr: "Délai minimal du début",
          ja: "開始イベントの最小待機時間",
          ru: "Минимальная задержка начала",
          vi: "Độ trễ bắt đầu tối thiểu",
        },
        description: {
          en: "Send upstream immediately and skip the start event if upstream data arrives first",
          zh: "立即请求上游，上游数据先到则跳过提前包",
          "zh-TW": "立即請求上游，上游資料先到則略過提前封包",
          fr: "Appel amont immédiat, sans événement anticipé si les données arrivent avant",
          ja: "上流へ即座にリクエストし、先にデータが届いた場合は開始イベントを省略",
          ru: "Запрос отправляется сразу; при раннем ответе событие начала пропускается",
          vi: "Gửi yêu cầu ngay, bỏ qua sự kiện bắt đầu nếu dữ liệu đến trước",
        },
        unit: "second",
        minimum: 0,
        maximum: 60,
        default: 1,
        visibleWhen: { timing: "before_upstream" },
      },
      delayMaxSeconds: {
        type: "number",
        title: {
          en: "Maximum start delay",
          zh: "提前包最大延迟",
          "zh-TW": "提前封包最長延遲",
          fr: "Délai maximal du début",
          ja: "開始イベントの最大待機時間",
          ru: "Максимальная задержка начала",
          vi: "Độ trễ bắt đầu tối đa",
        },
        description: {
          en: "Choose a random delay within the interval; equal bounds give a fixed delay",
          zh: "在区间内随机延迟，上下限相同则为固定延迟",
          "zh-TW": "在區間內隨機延遲，上下限相同則為固定延遲",
          fr: "Délai aléatoire dans cet intervalle, fixe si les bornes sont égales",
          ja: "範囲内でランダムに待機し、上下限が同じ場合は固定時間",
          ru: "Случайная задержка в интервале; равные границы задают фиксированную задержку",
          vi: "Chọn ngẫu nhiên trong khoảng; hai giới hạn bằng nhau tạo độ trễ cố định",
        },
        unit: "second",
        minimum: 0,
        maximum: 60,
        default: 2,
        visibleWhen: { timing: "before_upstream" },
      },
    },
  },
};

/** Reject reversed delay bounds when an instance is saved. */
export function validateConfig(config) {
  if (config.delayMinSeconds > config.delayMaxSeconds) {
    throw new Error("Minimum start delay must not exceed maximum start delay");
  }
}

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
  const config = ctx.config || {};
  const effect = { type: "stream.start", timing: config.timing || "after_connect" };
  if (effect.timing === "before_upstream") {
    const minMs = Math.ceil((config.delayMinSeconds ?? 1) * 1000);
    const maxMs = Math.max(minMs, Math.floor((config.delayMaxSeconds ?? 2) * 1000));
    effect.delayMs = minMs + Math.floor(Math.random() * (maxMs - minMs + 1));
  }
  return { action: "continue", effects: [effect] };
}
