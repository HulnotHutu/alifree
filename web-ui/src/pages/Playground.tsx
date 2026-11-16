import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Tabs,
  Button,
  Card,
  Input,
  Select,
  Space,
  Tag,
  Typography,
  Upload,
} from "antd";
import { InboxOutlined, SendOutlined } from "@ant-design/icons";
import { fetchKeyStatus, fetchQuota, fileToBase64, inferChat, inferEmbeddings, inferImageGen } from "../api";
import type { InferEnvelope, ModelQuota } from "../types";
import { fmtNum } from "../types";

const VIDEO_MODELS = new Set(["wan3.0-video-prime"]);

const TAB_DESC: Record<string, string> = {
  text: "纯文本对话：文本进 → 文本出。",
  vision: "图像/视频生成：文本进 → 图片或视频出（异步任务，提交后自动轮询）。",
  multimodal: "图文混合理解：图片（可选）+ 提问 → 文本出。",
  audio: "语音识别：音频进 → 文本出。",
  embedding: "向量化：文本进 → 向量出，比较两段文本的余弦相似度。",
};

type TabKey = "text" | "vision" | "multimodal" | "audio" | "embedding";

const PICK: Record<TabKey, (m: ModelQuota) => boolean> = {
  text: (m) => m.type === "Text",
  vision: (m) => ["qwen-image-2.1-turbo", "qwen-image-2.1-pro", "wan3.0-video-prime"].includes(m.model),
  multimodal: (m) => m.model === "qwen3.8-omni-flash",
  audio: (m) => ["qwen-audio-3.1-asr-flash", "qwen-audio-3.1-asr-flash-message"].includes(m.model),
  embedding: (m) => m.model.includes("text-embedding"),
};

const FALLBACK: Record<TabKey, string[]> = {
  text: ["qwen3.8-flash"],
  vision: ["qwen-image-2.1-turbo", "wan3.0-video-prime"],
  multimodal: ["qwen3.8-omni-flash"],
  audio: ["qwen-audio-3.1-asr-flash"],
  embedding: ["qwen3.7-text-embedding-flash"],
};

// 模型名与剩余额度分列显示：名称居左、额度居右
function modelOption(m: ModelQuota) {
  return {
    value: m.model,
    label: (
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: 12,
          minWidth: 0,
        }}
      >
        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {m.model}
        </span>
        <span style={{ flexShrink: 0, color: "#98A2B3", fontSize: 12.5 }}>
          {m.remaining != null ? `剩 ${fmtNum(m.remaining)}` : ""}
        </span>
      </div>
    ),
  };
}

export default function Playground() {
  const [models, setModels] = useState<ModelQuota[] | null>(null);
  const [keyOk, setKeyOk] = useState<boolean | null>(null);
  const [tab, setTab] = useState<TabKey>("text");

  useEffect(() => {
    fetchKeyStatus().then((out) => setKeyOk(out.configured)).catch(() => setKeyOk(false));
    fetchQuota().then((out) => {
      if (out.ok && out.data) setModels(out.data.models);
    }).catch(() => {});
  }, []);

  const modelsBy = useMemo(() => {
    const by: Record<string, ModelQuota[]> = {};
    for (const tk of Object.keys(PICK) as TabKey[]) {
      const list = models ? models.filter(PICK[tk]) : [];
      by[tk] = list.length ? list : (FALLBACK[tk] || []).map((model) => ({ model } as ModelQuota));
    }
    return by;
  }, [models]);

  function Err({ out }: { out: InferEnvelope | null }) {
    if (!out || out.ok) return null;
    return <Alert type="error" showIcon message={out.error} style={{ marginTop: 12 }} />;
  }

  function Latency({ out }: { out: InferEnvelope | null }) {
    if (!out?.ok) return null;
    return (
      <Space size={16} style={{ marginTop: 12 }}>
        <Tag className="mono">耗时 {out.latencyMs} ms</Tag>
        {out.data?.usage?.total_tokens != null && (
          <Tag className="mono">tokens {out.data.usage.total_tokens}</Tag>
        )}
      </Space>
    );
  }

  return (
    <Space direction="vertical" size={14} style={{ width: "100%" }}>
      {keyOk === false && (
        <Alert
          type="warning"
          showIcon
          message={
            <>
              尚未配置模型调用 Key。请在 <code>~/.config/bailian-quota.env</code> 追加一行{" "}
              <code>DASHSCOPE_API_KEY=sk-…</code>（百炼控制台「API-KEY」页面创建），保存后本页即可直接使用，无需重启。
            </>
          }
        />
      )}

      <Tabs
        activeKey={tab}
        onChange={(k: string) => setTab(k as TabKey)}
        items={[
          { key: "text", label: "文本 · 对话" },
          { key: "vision", label: "视觉 · 文生图" },
          { key: "multimodal", label: "全模态 · 图+文理解" },
          { key: "audio", label: "语音 · 识别" },
          { key: "embedding", label: "向量 · 相似度" },
        ]}
      />
      <Typography.Text type="secondary" style={{ fontSize: 13 }}>
        {TAB_DESC[tab]}
      </Typography.Text>

      {tab === "text" && <TextTab models={modelsBy.text} Err={Err} Latency={Latency} />}
      {tab === "vision" && <VisionTab models={modelsBy.vision} Err={Err} Latency={Latency} />}
      {tab === "multimodal" && <MultimodalTab models={modelsBy.multimodal} Err={Err} Latency={Latency} />}
      {tab === "audio" && <AudioTab models={modelsBy.audio} Err={Err} Latency={Latency} />}
      {tab === "embedding" && <EmbeddingTab models={modelsBy.embedding} Err={Err} Latency={Latency} />}
    </Space>
  );
}

function Pane({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }}>
      {children}
    </div>
  );
}

function OutCard({ children, title = "输出" }: { children: React.ReactNode; title?: string }) {
  return (
    <Card title={title} styles={{ body: { minHeight: 260 } }}>
      {children}
    </Card>
  );
}

function TextField({ label, value, onChange, rows = 4, placeholder }: {
  label: string; value: string; onChange: (v: string) => void; rows?: number; placeholder?: string;
}) {
  return (
    <>
      <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">{label}</Typography.Paragraph>
      <Input.TextArea
        rows={rows}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
    </>
  );
}

function TextTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [prompt, setPrompt] = useState("用一句话解释什么是“免费额度用完即停”。");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  function run() {
    if (!model) return;
    setLoading(true); setOut(null);
    inferChat({ model, messages: [{ role: "user", content: prompt }] })
      .then(setOut)
      .finally(() => setLoading(false));
  }

  const msg = out?.ok ? (out.data?.choices?.[0]?.message?.content || "(空响应)") : null;
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>模型</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={model} onChange={setModel}
                options={models.map(modelOption)} />
        <TextField label="提示词" value={prompt} onChange={setPrompt} />
        <Button type="primary" icon={<SendOutlined />} loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          发送
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          调用 OpenAI 兼容对话接口，消耗文本 token 额度。
        </Typography.Paragraph>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> : <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>{msg ?? "等待请求…"}</Typography.Paragraph>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}

function VisionTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [prompt, setPrompt] = useState("一只戴着宇航员头盔的橘猫，漂浮在星云中，赛博朋克风格");
  const [size, setSize] = useState("1024*1024");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);
  const kind = VIDEO_MODELS.has(model) ? "video" : "image";

  function run() {
    setLoading(true); setOut(null);
    inferImageGen({ model, prompt, size, kind: kind as "image" | "video" })
      .then(setOut)
      .finally(() => setLoading(false));
  }

  const urls: string[] = out?.ok ? out.data?.urls ?? [] : [];
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>模型（图像生成 / 视频生成）</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={model} onChange={setModel}
                options={models.map(modelOption)} />
        <TextField label="画面描述" value={prompt} onChange={setPrompt} />
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>尺寸</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={size} onChange={setSize}
                options={["1024*1024", "720*1280", "1280*720"].map((v) => ({ value: v, label: v.replace("*", "×") }))} />
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          生成{kind === "video" ? "视频（1–4 分钟）" : "图片（10–30 秒）"}
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          异步任务提交后自动轮询；产物 URL 有效期有限，及时保存。
        </Typography.Paragraph>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> :
          urls.length ? (
            kind === "video"
              ? urls.map((u) => <video key={u} src={u} controls style={{ maxWidth: "100%", borderRadius: 8 }} />)
              : urls.map((u) => <img key={u} src={u} alt="生成结果" style={{ maxWidth: "100%", borderRadius: 8 }} />)
          ) : <Typography.Text type="secondary">等待请求…</Typography.Text>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}

function MultimodalTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [prompt, setPrompt] = useState("请用两句话描述这张图片。");
  const [file, setFile] = useState<File | null>(null);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  async function run() {
    setLoading(true); setOut(null);
    try {
      const content: any[] = [];
      if (file) {
        if (file.size > 10 * 1024 * 1024) throw new Error("图片超过 10MB。");
        const dataUrl = await fileToBase64(file);
        content.push({ type: "image_url", image_url: { url: dataUrl } });
      }
      content.push({ type: "text", text: prompt });
      const out = await inferChat({ model, messages: [{ role: "user", content }], modalities: ["text"] });
      setOut(out);
    } catch (e: any) {
      setOut({ ok: false, error: e.message });
    } finally {
      setLoading(false);
    }
  }

  const msg = out?.ok ? (out.data?.choices?.[0]?.message?.content || "(空响应)") : null;
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>模型</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={model} onChange={setModel}
                options={models.map(modelOption)} />
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4, marginTop: 10 }}>图片（可选，png/jpg ≤ 10MB）</Typography.Paragraph>
        <Upload.Dragger
          maxCount={1}
          accept="image/png,image/jpeg,image/webp"
          showUploadList={!!file}
          beforeUpload={(f) => { setFile(f); return false; }}
          onRemove={() => setFile(null)}
        >
          <InboxOutlined style={{ fontSize: 32, color: "#98A2B3" }} />
          <Typography.Paragraph style={{ margin: "8px 0 0" }}>点击或拖拽图片到这里</Typography.Paragraph>
        </Upload.Dragger>
        <TextField label="提问" value={prompt} onChange={setPrompt} />
        <Button type="primary" icon={<SendOutlined />} loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          发送
        </Button>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> : <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>{msg ?? "等待请求…"}</Typography.Paragraph>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}

function AudioTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [prompt, setPrompt] = useState("请把这段语音转写成文字。");
  const [file, setFile] = useState<File | null>(null);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  async function run() {
    if (!file) { setOut({ ok: false, error: "请选择音频文件（wav/mp3）。" }); return; }
    if (file.size > 10 * 1024 * 1024) { setOut({ ok: false, error: "音频超过 10MB。" }); return; }
    setLoading(true); setOut(null);
    try {
      const dataUrl = await fileToBase64(file);
      const base64 = dataUrl.split(",")[1] || "";
      const format = file.name.toLowerCase().endsWith(".mp3") ? "mp3" : "wav";
      const out = await inferChat({
        model,
        messages: [{
          role: "user",
          content: [
            { type: "input_audio", input_audio: { data: base64, format } },
            { type: "text", text: prompt },
          ],
        }],
      });
      setOut(out);
    } catch (e: any) {
      setOut({ ok: false, error: e.message });
    } finally {
      setLoading(false);
    }
  }

  const msg = out?.ok ? (out.data?.choices?.[0]?.message?.content || "(空响应)") : null;
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>模型（语音识别）</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={model} onChange={setModel}
                options={models.map(modelOption)} />
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4, marginTop: 10 }}>音频文件（wav/mp3 ≤ 10MB）</Typography.Paragraph>
        <Upload.Dragger
          maxCount={1}
          accept="audio/wav,audio/mpeg"
          showUploadList={!!file}
          beforeUpload={(f) => { setFile(f); return false; }}
          onRemove={() => setFile(null)}
        >
          <InboxOutlined style={{ fontSize: 32, color: "#98A2B3" }} />
          <Typography.Paragraph style={{ margin: "8px 0 0" }}>点击或拖拽音频到这里</Typography.Paragraph>
        </Upload.Dragger>
        <TextField label="附加指令（可选）" value={prompt} onChange={setPrompt} rows={2} />
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          识别
        </Button>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> : <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>{msg ?? "等待请求…"}</Typography.Paragraph>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}

function EmbeddingTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [a, setA] = useState("阿里巴巴的百炼平台提供大模型服务。");
  const [b, setB] = useState("阿里云上可以调用通义千问系列的模型。");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  function run() {
    if (!a.trim() || !b.trim()) { setOut({ ok: false, error: "请输入两段文本。" }); return; }
    setLoading(true); setOut(null);
    inferEmbeddings(model, [a, b]).then(setOut).finally(() => setLoading(false));
  }

  const vecs: number[][] = out?.ok ? (out.data?.data ?? []).map((e: any) => e.embedding) : [];
  const sim = vecs.length >= 2
    ? vecs[0].reduce((s, v, i) => s + v * vecs[1][i], 0) /
      (Math.sqrt(vecs[0].reduce((s, x) => s + x * x, 0)) * Math.sqrt(vecs[1].reduce((s, x) => s + x * x, 0)))
    : null;

  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>模型</Typography.Paragraph>
        <Select style={{ width: "100%" }} value={model} onChange={setModel}
                options={models.map(modelOption)} />
        <TextField label="文本 A" value={a} onChange={setA} />
        <TextField label="文本 B" value={b} onChange={setB} rows={2} />
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          计算向量与相似度
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          输出每个文本的向量（展示维度与前几维），并计算 A、B 的余弦相似度。
        </Typography.Paragraph>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> :
          sim !== null ? (
            <>
              <div style={{ fontSize: 30, fontWeight: 650, fontVariantNumeric: "tabular-nums" }}>
                {sim.toFixed(4)}
              </div>
              <Typography.Text type="secondary">余弦相似度（-1 ~ 1，越接近 1 越相似）</Typography.Text>
              <Typography.Paragraph className="mono" type="secondary" style={{ fontSize: 12.5, marginTop: 10, whiteSpace: "pre-wrap" }}>
                {`维度：${vecs[0].length}\nA 前 8 维：[${vecs[0].slice(0, 8).map((x) => x.toFixed(4)).join(", ")}…]\nB 前 8 维：[${vecs[1].slice(0, 8).map((x) => x.toFixed(4)).join(", ")}…]`}
              </Typography.Paragraph>
            </>
          ) : <Typography.Text type="secondary">等待请求…</Typography.Text>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}
