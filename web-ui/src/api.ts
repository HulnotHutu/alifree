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

export async function inferChat(
  req: { model: string; messages: ChatMessage[]; modalities?: string[] },
): Promise<InferEnvelope> {
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
): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/embeddings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ model, inputs }),
  });
  return res.json();
}

export async function inferImageGen(req: {
  model: string;
  prompt: string;
  size?: string;
  kind: "image" | "video";
}): Promise<InferEnvelope> {
  const res = await fetch("/api/infer/imagegen", {
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
