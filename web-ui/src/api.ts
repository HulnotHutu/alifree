import type {
  ChatMessage,
  InferEnvelope,
  QuotaResponse,
  RefreshResponse,
  RunInfo,
} from "./types";

// 统一的 API 客户端：开发期经 Vite 代理到 Go 后端，生产同源。

export async function fetchQuota(): Promise<QuotaResponse> {
  const res = await fetch("/api/quota");
  return res.json();
}

export async function fetchHistory(): Promise<RunInfo[]> {
  const res = await fetch("/api/history");
  return res.json();
}

export async function refreshQuota(): Promise<RefreshResponse> {
  const res = await fetch("/api/refresh", { method: "POST" });
  return res.json();
}

export async function fetchKeyStatus(): Promise<{ configured: boolean }> {
  const res = await fetch("/api/key-status");
  return res.json();
}

// 对话接口：透传完整 OpenAI 兼容请求体（temperature/top_p/max_tokens/
// enable_thinking/asr_options 等官方参数直接放在顶层）。
export async function inferChat(req: {
  model: string;
  messages: ChatMessage[];
  [key: string]: unknown;
}): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function inferEmbeddings(
  model: string,
  inputs: string[],
  dimensions?: number,
): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/embeddings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ model, inputs, dimensions }),
  });
  return res.json();
}

export async function inferImageGen(req: {
  model: string;
  prompt: string;
  kind: "image" | "video";
  params?: Record<string, unknown>; // 官方 parameters 对象
}): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/imagegen", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

// 非实时语音合成：input 为官方 input 对象（text/voice/format/sample_rate…）。
export async function inferTTS(req: {
  model: string;
  input: Record<string, unknown>;
}): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/tts", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

// 排序模型：params 为官方 parameters 对象（top_n/return_documents/instruct）。
export async function inferRerank(req: {
  model: string;
  query: string;
  documents: string[];
  params?: Record<string, unknown>;
}): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/rerank", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(String(fr.result));
    fr.onerror = () => reject(new Error("读取文件失败"));
    fr.readAsDataURL(file);
  });
}
