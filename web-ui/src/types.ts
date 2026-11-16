// 与 Go 后端（internal/app、internal/bailian）的 JSON 契约保持一致的类型。

export interface ModelQuota {
  model: string;
  type: string; // Text / Vision / Multimodal / Audio / Embedding / "-"
  remaining: number;
  total: number;
  remainingPercent: number;
  expires: string;
  freeTierOnly?: boolean; // 用完即停开关；undefined 表示未知
}

export interface Snapshot {
  fetched_at: string;
  source: string;
  summary: { totalModels: number; withQuota: number };
  models: ModelQuota[];
}

export interface QuotaMeta {
  db?: string;
  snapshotId?: number;
  ageHours?: number;
  credentials: boolean;
}

export interface QuotaResponse {
  ok: boolean;
  error?: string;
  data?: Snapshot;
  meta?: QuotaMeta;
}

export interface RunInfo {
  id: number;
  fetchedAt: string;
  totalModels: number;
  withQuota: number;
}

export interface RefreshResponse extends QuotaResponse {}

export interface InferEnvelope {
  ok: boolean;
  error?: string;
  code?: string;
  data?: any;
  latencyMs?: number;
}

export interface ChatMessage {
  role: "user" | "assistant" | "system";
  content:
    | string
    | Array<
        | { type: "text"; text: string }
        | { type: "image_url"; image_url: { url: string } }
        | { type: "input_audio"; input_audio: { data: string; format: string } }
      >;
}

export const TYPE_COLORS: Record<string, string> = {
  Text: "#2E90FA",
  Vision: "#875BF7",
  Audio: "#EE46BC",
  Embedding: "#12B5A2",
  Multimodal: "#F79009",
  "-": "#98A2B3",
};

export const TYPE_LABEL: Record<string, string> = { "-": "其他" };

export function fmtNum(v: number): string {
  const n = Math.round(Number(v) || 0);
  if (n >= 1_000_000 && n % 100_000 === 0) return n / 1_000_000 + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1).replace(/\.0$/, "") + "K";
  return String(n);
}
