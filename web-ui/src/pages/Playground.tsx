import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  Collapse,
  Input,
  InputNumber,
  Select,
  Space,
  Slider,
  Switch,
  Tabs,
  Tag,
  Typography,
  Upload,
} from "antd";
import { InboxOutlined, SendOutlined } from "@ant-design/icons";
import {
  fetchKeyStatus,
  fetchQuota,
  fileToBase64,
  inferChat,
  inferEmbeddings,
  inferImageGen,
  inferRerank,
  inferTTS,
} from "../api";
import type { ChatMessage, InferEnvelope, ModelQuota } from "../types";
import { fmtNum } from "../types";

const VIDEO_MODELS = new Set(["wan3.0-video-prime"]);

const TAB_DESC: Record<string, string> = {
  text: "纯文本对话：文本进 → 文本出。",
  vision: "图像/视频生成：文本进 → 图片或视频出（异步任务，提交后自动轮询）。",
  multimodal: "图文混合理解：图片（可选）+ 提问 → 文本出。",
  audio: "语音识别：音频进 → 文本出。",
  tts: "语音合成：文本进 → 音频出（官方 HTTP 接口，返回音频 URL）。",
  embedding: "向量化：文本进 → 向量出，比较两段文本的余弦相似度。",
  rerank: "排序：查询 + 候选文档 → 按相关性得分从高到低排序。",
};

type TabKey = "text" | "vision" | "multimodal" | "audio" | "tts" | "embedding" | "rerank";

const PICK: Record<TabKey, (m: ModelQuota) => boolean> = {
  text: (m) => m.type === "Text",
  vision: (m) => ["qwen-image-2.1-turbo", "qwen-image-2.1-pro", "wan3.0-video-prime"].includes(m.model),
  multimodal: (m) => m.model === "qwen3.8-omni-flash",
  audio: (m) => ["qwen-audio-3.1-asr-flash", "qwen-audio-3.1-asr-flash-message"].includes(m.model),
  tts: (m) => /tts|cosyvoice/i.test(m.model),
  embedding: (m) => m.model.includes("text-embedding"),
  rerank: (m) => m.model.includes("rerank"),
};

const FALLBACK: Record<TabKey, string[]> = {
  text: ["qwen3.8-flash"],
  vision: ["qwen-image-2.1-turbo", "wan3.0-video-prime"],
  multimodal: ["qwen3.8-omni-flash"],
  audio: ["qwen-audio-3.1-asr-flash"],
  tts: ["qwen3-tts-flash", "qwen-audio-3.1-tts-flash", "cosyvoice-v3-flash", "cosyvoice-v2"],
  embedding: ["qwen3.7-text-embedding-flash"],
  rerank: ["qwen3.7-text-rerank", "qwen3-rerank", "gte-rerank-v2"],
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

function ModelSelect({
  value,
  onChange,
  models,
}: {
  value: string;
  onChange: (v: string) => void;
  models: ModelQuota[];
}) {
  return (
    <Select
      style={{ width: "100%" }}
      value={value}
      onChange={onChange}
      options={models.map(modelOption)}
    />
  );
}

// 高级参数折叠区（按官方 API 文档提供可配置项）
function Advanced({ children }: { children: React.ReactNode }) {
  return (
    <Collapse
      ghost
      size="small"
      style={{ marginTop: 10, marginLeft: -8 }}
      items={[{ key: "adv", label: "高级参数（按官方 API 文档）", children }]}
    />
  );
}

// 采样参数控件（chat 类模型通用）
function Sampling({ temp, setTemp, topP, setTopP, defaults }: {
  temp: number;
  setTemp: (v: number) => void;
  topP: number;
  setTopP: (v: number) => void;
  defaults: { temp: number; topP: number };
}) {
  return (
    <Space direction="vertical" size={4} style={{ width: "100%" }}>
      <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
        temperature（0–2，官方默认 {defaults.temp}）
      </Typography.Text>
      <Slider min={0} max={1.99} step={0.01} value={temp} onChange={setTemp} />
      <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
        top_p（0–1，官方默认 {defaults.topP}）
      </Typography.Text>
      <Slider min={0.01} max={1} step={0.01} value={topP} onChange={setTopP} />
    </Space>
  );
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
          { key: "tts", label: "语音 · 合成" },
          { key: "embedding", label: "向量 · 相似度" },
          { key: "rerank", label: "排序 · 相关性" },
        ]}
      />
      <Typography.Text type="secondary" style={{ fontSize: 13 }}>
        {TAB_DESC[tab]}
      </Typography.Text>

      {tab === "text" && <TextTab models={modelsBy.text} Err={Err} Latency={Latency} />}
      {tab === "vision" && <VisionTab models={modelsBy.vision} Err={Err} Latency={Latency} />}
      {tab === "multimodal" && <MultimodalTab models={modelsBy.multimodal} Err={Err} Latency={Latency} />}
      {tab === "audio" && <AudioTab models={modelsBy.audio} Err={Err} Latency={Latency} />}
      {tab === "tts" && <TTSTab models={modelsBy.tts} Err={Err} Latency={Latency} />}
      {tab === "embedding" && <EmbeddingTab models={modelsBy.embedding} Err={Err} Latency={Latency} />}
      {tab === "rerank" && <RerankTab models={modelsBy.rerank} Err={Err} Latency={Latency} />}
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
  label: string;
  value: string;
  onChange: (v: string) => void;
  rows?: number;
  placeholder?: string;
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
  const [prompt, setPrompt] = useState("");
  const [temp, setTemp] = useState(0.7);
  const [topP, setTopP] = useState(0.8);
  const [maxTokens, setMaxTokens] = useState<number | null>(null);
  const [thinking, setThinking] = useState(false);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  function run() {
    if (!model) return;
    if (!prompt.trim()) { setOut({ ok: false, error: "请输入提示词。" }); return; }
    setLoading(true); setOut(null);
    const body: { model: string; messages: ChatMessage[]; [key: string]: unknown } = {
      model,
      messages: [{ role: "user", content: prompt }],
      temperature: temp,
      top_p: topP,
    };
    if (maxTokens) body.max_tokens = maxTokens;
    if (thinking) body.enable_thinking = true;
    inferChat(body).then(setOut).finally(() => setLoading(false));
  }

  const msg = out?.ok ? (out.data?.choices?.[0]?.message?.content || "(空响应)") : null;
  const reasoning = out?.ok ? out.data?.choices?.[0]?.message?.reasoning_content : null;
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <TextField label="提示词" value={prompt} onChange={setPrompt}
                   placeholder="请输入提示词，例如：用一句话介绍你自己" />
        <Advanced>
          <Sampling temp={temp} setTemp={setTemp} topP={topP} setTopP={setTopP}
                    defaults={{ temp: 0.7, topP: 0.8 }} />
          <div style={{ marginTop: 10 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>max_tokens（留空为模型默认）</Typography.Text>
            <InputNumber min={1} style={{ width: "100%", marginTop: 4 }}
                         value={maxTokens} onChange={(v) => setMaxTokens(v ?? null)} />
          </div>
          <div style={{ marginTop: 10 }}>
            <Space>
              <Switch size="small" checked={thinking} onChange={setThinking} />
              <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
                enable_thinking（混合思考；开启后思维链走 reasoning_content）
              </Typography.Text>
            </Space>
          </div>
        </Advanced>
        <Button type="primary" icon={<SendOutlined />} loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          发送
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          调用 OpenAI 兼容对话接口，消耗文本 token 额度。
        </Typography.Paragraph>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> : (
          <>
            {reasoning ? (
              <Typography.Paragraph style={{ whiteSpace: "pre-wrap", fontSize: 12.5, color: "#98A2B3",
                borderLeft: "3px solid #E3E7ED", paddingLeft: 10 }}>
                {reasoning}
              </Typography.Paragraph>
            ) : null}
            <Typography.Paragraph style={{ whiteSpace: "pre-wrap" }}>{msg ?? "等待请求…"}</Typography.Paragraph>
          </>
        )}
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
  const [prompt, setPrompt] = useState("");
  const [size, setSize] = useState("");
  const [negative, setNegative] = useState("");
  const [extend, setExtend] = useState(true);
  const [watermark, setWatermark] = useState(false);
  const [seed, setSeed] = useState<number | null>(null);
  // wan3.0 视频参数
  const [resolution, setResolution] = useState("1080P");
  const [ratio, setRatio] = useState("adaptive");
  const [duration, setDuration] = useState(5);
  const [audio, setAudio] = useState(true);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);
  const kind = VIDEO_MODELS.has(model) ? "video" : "image";

  function run() {
    if (!model) return;
    if (!prompt.trim()) { setOut({ ok: false, error: "请输入画面描述。" }); return; }
    setLoading(true); setOut(null);
    const params: Record<string, any> = { prompt_extend: extend, watermark };
    if (kind === "video") {
      params.resolution = resolution;
      params.ratio = ratio;
      params.duration = duration;
      params.audio = audio;
    } else if (size) {
      params.size = size;
    }
    if (seed != null && seed >= 0) params.seed = seed;
    inferImageGen({ model, prompt, kind: kind as "image" | "video", params })
      .then(setOut)
      .finally(() => setLoading(false));
  }

  const urls: string[] = out?.ok ? out.data?.urls ?? [] : [];
  const isVideo = kind === "video";
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型（图像生成 / 视频生成）</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <TextField label="画面描述" value={prompt} onChange={setPrompt}
                   placeholder="例如：一只戴着宇航员头盔的橘猫，漂浮在星云中，赛博朋克风格" />
        {!isVideo && (
          <>
            <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">尺寸</Typography.Paragraph>
            <Select style={{ width: "100%" }} value={size} onChange={setSize}
                    options={[
                      { value: "", label: "跟随模型默认" },
                      { value: "2048*2048", label: "2048×2048（1:1）" },
                      { value: "2688*1536", label: "2688×1536（16:9）" },
                      { value: "1536*2688", label: "1536×2688（9:16）" },
                      { value: "1728*2368", label: "1728×2368（3:4）" },
                    ]} />
          </>
        )}
        <Advanced>
          {!isVideo && (
            <TextField label="反向提示词 negative_prompt（≤500 字符）" value={negative} onChange={setNegative} rows={2}
                       placeholder="不希望出现的内容，例如：低分辨率，低画质" />
          )}
          {isVideo && (
            <>
              <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">分辨率 resolution（官方默认 1080P）</Typography.Paragraph>
              <Select style={{ width: "100%" }} value={resolution} onChange={setResolution}
                      options={["1080P", "720P", "480P"].map((v) => ({ value: v, label: v }))} />
              <Typography.Paragraph style={{ marginBottom: 4, marginTop: 10 }} type="secondary">宽高比 ratio（官方默认 adaptive）</Typography.Paragraph>
              <Select style={{ width: "100%" }} value={ratio} onChange={setRatio}
                      options={["adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"].map((v) => ({ value: v, label: v }))} />
              <div style={{ marginTop: 10 }}>
                <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>时长 duration 秒（2–30，官方默认 5）</Typography.Text>
                <InputNumber min={-1} max={30} style={{ width: "100%", marginTop: 4 }}
                             value={duration} onChange={(v) => setDuration(v ?? 5)} />
              </div>
              <div style={{ marginTop: 10 }}>
                <Space>
                  <Switch size="small" checked={audio} onChange={setAudio} />
                  <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>audio（生成音轨，官方默认 true）</Typography.Text>
                </Space>
              </div>
            </>
          )}
          <div style={{ marginTop: 10 }}>
            <Space>
              <Switch size="small" checked={extend} onChange={setExtend} />
              <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>prompt_extend（智能改写提示词，官方默认 true）</Typography.Text>
            </Space>
          </div>
          <div style={{ marginTop: 10 }}>
            <Space>
              <Switch size="small" checked={watermark} onChange={setWatermark} />
              <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>watermark（水印，官方默认 false）</Typography.Text>
            </Space>
          </div>
          <div style={{ marginTop: 10 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>seed（留空随机）</Typography.Text>
            <InputNumber min={0} max={2147483647} style={{ width: "100%", marginTop: 4 }}
                         value={seed} onChange={(v) => setSeed(v ?? null)} />
          </div>
        </Advanced>
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          生成{isVideo ? "视频（1–4 分钟）" : "图片（10–30 秒）"}
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          异步任务提交后自动轮询；产物 URL 有效期 24 小时，及时保存。
        </Typography.Paragraph>
      </Card>
      <OutCard>
        {out && !out.ok ? <Err out={out} /> :
          urls.length ? (
            (kind === "video"
              ? urls.map((u) => <video key={u} src={u} controls style={{ maxWidth: "100%", borderRadius: 8 }} />)
              : urls.map((u) => <img key={u} src={u} alt="生成结果" style={{ maxWidth: "100%", borderRadius: 8, marginBottom: 8 }} />))
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
  const [prompt, setPrompt] = useState("");
  const [temp, setTemp] = useState(0.7);
  const [topP, setTopP] = useState(0.8);
  const [file, setFile] = useState<File | null>(null);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  async function run() {
    setLoading(true); setOut(null);
    try {
      if (!prompt.trim()) throw new Error("请输入提问。");
      const content: any[] = [];
      if (file) {
        if (file.size > 10 * 1024 * 1024) throw new Error("图片超过 10MB。");
        const dataUrl = await fileToBase64(file);
        content.push({ type: "image_url", image_url: { url: dataUrl } });
      }
      content.push({ type: "text", text: prompt });
      const out = await inferChat({
        model, messages: [{ role: "user", content }], modalities: ["text"],
        temperature: temp, top_p: topP,
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
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <Typography.Paragraph style={{ marginBottom: 4, marginTop: 10 }} type="secondary">图片（可选，png/jpg ≤ 10MB）</Typography.Paragraph>
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
        <TextField label="提问" value={prompt} onChange={setPrompt}
                   placeholder="例如：请用两句话描述这张图片。" />
        <Advanced>
          <Sampling temp={temp} setTemp={setTemp} topP={topP} setTopP={setTopP}
                    defaults={{ temp: 0.7, topP: 0.8 }} />
        </Advanced>
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
  const [prompt, setPrompt] = useState("");
  const [language, setLanguage] = useState("");
  const [itn, setItn] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  async function run() {
    if (!file) { setOut({ ok: false, error: "请选择音频文件（wav/mp3）。" }); return; }
    if (file.size > 10 * 1024 * 1024) { setOut({ ok: false, error: "音频超过 10MB。" }); return; }
    setLoading(true); setOut(null);
    try {
      const dataUrl = await fileToBase64(file);
      // 官方协议：data 为完整 Data URI（data:audio/wav;base64,<data>）或音频 URL，无 format 字段
      const asrOptions: Record<string, any> = { enable_itn: itn };
      if (language) asrOptions.language = language;
      const body: { model: string; messages: ChatMessage[]; [key: string]: unknown } = {
        model,
        messages: [{
          role: "user",
          content: [
            { type: "input_audio", input_audio: { data: dataUrl } },
            { type: "text", text: prompt || "请转写这段音频。" },
          ],
        }],
        asr_options: asrOptions,
      };
      const out = await inferChat(body);
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
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型（语音识别）</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <Typography.Paragraph style={{ marginBottom: 4, marginTop: 10 }} type="secondary">音频文件（wav/mp3 ≤ 10MB）</Typography.Paragraph>
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
        <TextField label="附加指令（可选）" value={prompt} onChange={setPrompt} rows={2}
                   placeholder="可选：例如 请把这段语音转写成文字" />
        <Advanced>
          <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">language（音频语言；官方默认自动检测）</Typography.Paragraph>
          <Select style={{ width: "100%" }} value={language} onChange={setLanguage}
                  options={[{ value: "", label: "自动检测" },
                    ...["zh", "en", "ja", "ko", "de", "es", "fr", "it", "pt", "ru", "ar"].map((v) => ({ value: v, label: v }))]} />
          <div style={{ marginTop: 10 }}>
            <Space>
              <Switch size="small" checked={itn} onChange={setItn} />
              <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
                enable_itn（逆文本正则化，仅中英文；官方默认 false）
              </Typography.Text>
            </Space>
          </div>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 10 }}>
            热词/上下文（context）官方要求通过 System Message 提供，本页暂未开放。
          </Typography.Paragraph>
        </Advanced>
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
  const [dimensions, setDimensions] = useState(1024);
  const [a, setA] = useState("");
  const [b, setB] = useState("");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  function run() {
    if (!a.trim() || !b.trim()) { setOut({ ok: false, error: "请输入两段文本。" }); return; }
    setLoading(true); setOut(null);
    inferEmbeddings(model, [a, b], dimensions).then(setOut).finally(() => setLoading(false));
  }

  const vecs: number[][] = out?.ok ? (out.data?.data ?? []).map((e: any) => e.embedding) : [];
  const sim = vecs.length >= 2
    ? vecs[0].reduce((s, v, i) => s + v * vecs[1][i], 0) /
      (Math.sqrt(vecs[0].reduce((s, x) => s + x * x, 0)) * Math.sqrt(vecs[1].reduce((s, x) => s + x * x, 0)))
    : null;

  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <TextField label="文本 A" value={a} onChange={setA}
                   placeholder="输入第一段文本，例如：阿里巴巴的百炼平台提供大模型服务" />
        <TextField label="文本 B" value={b} onChange={setB} rows={2}
                   placeholder="输入第二段文本，例如：阿里云上可以调用通义千问系列的模型" />
        <Advanced>
          <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">
            dimensions（输出维度，官方默认 1024；flash 系列支持 1024/768/512/256）
          </Typography.Paragraph>
          <Select style={{ width: "100%" }} value={dimensions} onChange={setDimensions}
                  options={[1024, 768, 512, 256].map((v) => ({ value: v, label: v }))} />
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 10 }}>
            text_type（query/document）与 instruct 仅 DashScope 原生接口支持，OpenAI 兼容模式不可用，故未开放。
          </Typography.Paragraph>
        </Advanced>
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          计算向量与相似度
        </Button>
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

// --- 语音合成（官方 HTTP 接口 /api/v1/services/audio/tts/SpeechSynthesizer）---

// 按模型家族给出官方音色列表页中验证过的音色；允许手动输入其他音色。
const VOICES: { match: RegExp; voices: [string, string][] }[] = [
  {
    match: /qwen3-tts/,
    voices: [
      ["Cherry", "芊悦 · 阳光积极女声"],
      ["Serena", "苏瑶 · 温柔女声"],
      ["Ethan", "晨煦 · 阳光男声"],
      ["Chelsie", "千雪 · 动漫女声"],
      ["Neil", "阿闻 · 新闻主播男声"],
      ["Dylan", "晓东 · 北京口音男声"],
      ["Kiki", "阿清 · 粤语女声"],
    ],
  },
  { match: /qwen-audio-3\.1-tts/, voices: [["longanhuan_v3.1", "龙安欢"]] },
  { match: /cosyvoice-v3\.5/, voices: [["longanyang", "龙安洋"]] },
  {
    match: /cosyvoice-v3/,
    voices: [
      ["longanyang", "龙安洋"],
      ["longanhuan", "龙安欢"],
      ["longhuhu_v3", "龙呼呼 · 童声"],
      ["loongbella_v3", "Bella 3.0"],
    ],
  },
  {
    match: /cosyvoice-v2/,
    voices: [
      ["longxiaochun_v2", "龙小淳"],
      ["longxiaoxia_v2", "龙小夏"],
    ],
  },
];

function voiceOptions(model: string): [string, string][] {
  return VOICES.find((v) => v.match.test(model))?.voices ?? [["longanyang", "龙安洋"]];
}

function TTSTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [text, setText] = useState("");
  const [voice, setVoice] = useState(voiceOptions(models[0]?.model ?? "")[0][0]);
  const [format, setFormat] = useState("wav");
  const [sampleRate, setSampleRate] = useState(24000);
  const [instruction, setInstruction] = useState("");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);

  function pickModel(m: string) {
    setModel(m);
    setVoice(voiceOptions(m)[0][0]); // 音色必须与模型匹配，切模型时重置为推荐音色
  }

  function run() {
    if (!model) return;
    if (!text.trim()) { setOut({ ok: false, error: "请输入要合成的文本。" }); return; }
    if (!voice.trim()) { setOut({ ok: false, error: "请填写音色 voice。" }); return; }
    setLoading(true); setOut(null);
    const input: Record<string, any> = { text, voice, format, sample_rate: sampleRate };
    if (instruction.trim()) input.instruction = instruction.trim();
    inferTTS({ model, input }).then(setOut).finally(() => setLoading(false));
  }

  const url: string | null = out?.ok ? out.data?.urls?.[0] ?? null : null;
  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型（语音合成）</Typography.Paragraph>
        <ModelSelect value={model} onChange={pickModel} models={models} />
        <TextField label="待合成文本" value={text} onChange={setText}
                   placeholder="例如：今天天气真不错，适合出去走走。" />
        <Typography.Paragraph style={{ marginBottom: 4, marginTop: 10 }} type="secondary">
          音色 voice（需与模型匹配，可手动输入官方音色列表中的其他音色）
        </Typography.Paragraph>
        <AutoComplete
          style={{ width: "100%" }}
          value={voice}
          onChange={setVoice}
          options={voiceOptions(model).map(([value, label]) => ({ value, label }))}
        />
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10, marginTop: 10 }}>
          <div>
            <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">音频格式 format</Typography.Paragraph>
            <Select style={{ width: "100%" }} value={format} onChange={setFormat}
                    options={["wav", "mp3", "pcm"].map((v) => ({ value: v, label: v }))} />
          </div>
          <div>
            <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">采样率 sample_rate</Typography.Paragraph>
            <Select style={{ width: "100%" }} value={sampleRate} onChange={setSampleRate}
                    options={[24000, 22050, 16000, 8000].map((v) => ({ value: v, label: v }))} />
          </div>
        </div>
        <Advanced>
          <TextField label="instruction 说话风格指令（仅部分模型支持）" value={instruction} onChange={setInstruction} rows={2}
                     placeholder="可选：用自然语言描述风格，例如：用轻快愉悦的语气说" />
        </Advanced>
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          合成
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          调用非实时语音合成 HTTP 接口，返回音频 URL（有时效性，及时保存）。
        </Typography.Paragraph>
      </Card>
      <OutCard title="输出（音频）">
        {out && !out.ok ? <Err out={out} /> :
          url ? (
            <>
              <audio controls src={url} style={{ width: "100%" }} />
              <Typography.Paragraph className="mono" style={{ fontSize: 12.5, marginTop: 10, wordBreak: "break-all" }}>
                <a href={url} target="_blank" rel="noreferrer">{url}</a>
              </Typography.Paragraph>
            </>
          ) : <Typography.Text type="secondary">等待请求…</Typography.Text>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}

// --- 排序（官方接口 /api/v1/services/rerank/text-rerank/text-rerank）---

function RerankTab({ models, Err, Latency }: {
  models: ModelQuota[];
  Err: (p: { out: InferEnvelope | null }) => React.ReactNode;
  Latency: (p: { out: InferEnvelope | null }) => React.ReactNode;
}) {
  const [model, setModel] = useState(models[0]?.model ?? "");
  const [query, setQuery] = useState("");
  const [docs, setDocs] = useState("");
  const [topN, setTopN] = useState<number | null>(null);
  const [returnDocs, setReturnDocs] = useState(true);
  const [instruct, setInstruct] = useState("");
  const [out, setOut] = useState<InferEnvelope | null>(null);
  const [loading, setLoading] = useState(false);
  const documents = useMemo(
    () => docs.split("\n").map((s) => s.trim()).filter(Boolean),
    [docs],
  );

  function run() {
    if (!model) return;
    if (!query.trim()) { setOut({ ok: false, error: "请输入查询 query。" }); return; }
    if (!documents.length) { setOut({ ok: false, error: "请输入至少一条候选文档（每行一条）。" }); return; }
    setLoading(true); setOut(null);
    const params: Record<string, any> = { return_documents: returnDocs };
    if (topN && topN > 0) params.top_n = topN;
    if (instruct.trim()) params.instruct = instruct.trim();
    inferRerank({ model, query, documents, params }).then(setOut).finally(() => setLoading(false));
  }

  type RerankResult = { index: number; relevance_score: number; document?: { text?: string } };
  const results: RerankResult[] = out?.ok ? out.data?.output?.results ?? [] : [];

  return (
    <Pane>
      <Card title="输入">
        <Typography.Paragraph style={{ marginBottom: 4 }} type="secondary">模型（排序）</Typography.Paragraph>
        <ModelSelect value={model} onChange={setModel} models={models} />
        <TextField label="查询 query" value={query} onChange={setQuery} rows={2}
                   placeholder="例如：什么是文本排序模型" />
        <TextField label="候选文档 documents（每行一条）" value={docs} onChange={setDocs} rows={6}
                   placeholder={"第一行一条候选文档，例如：\n文本排序模型广泛用于搜索引擎和推荐系统中\n量子计算是计算科学的一个前沿领域"} />
        <Advanced>
          <div style={{ marginTop: 4 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>top_n（只返回前 N 条，留空返回全部）</Typography.Text>
            <InputNumber min={1} style={{ width: "100%", marginTop: 4 }}
                         value={topN} onChange={(v) => setTopN(v ?? null)} />
          </div>
          <div style={{ marginTop: 10 }}>
            <Space>
              <Switch size="small" checked={returnDocs} onChange={setReturnDocs} />
              <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
                return_documents（结果中带回原文；qwen3 / gte-rerank 系列支持）
              </Typography.Text>
            </Space>
          </div>
          <TextField label="instruct 任务指令（仅 qwen3 系列排序模型支持）" value={instruct} onChange={setInstruct} rows={2}
                     placeholder="可选：例如 Given a web search query, retrieve relevant passages" />
        </Advanced>
        <Button type="primary" loading={loading} onClick={run} style={{ marginTop: 14 }} block>
          排序
        </Button>
        <Typography.Paragraph type="secondary" style={{ marginTop: 10, fontSize: 12.5 }}>
          调用排序接口，结果按 relevance_score（0–1）从高到低排列。
        </Typography.Paragraph>
      </Card>
      <OutCard title="输出（按相关性排序）">
        {out && !out.ok ? <Err out={out} /> :
          results.length ? (
            <Space direction="vertical" size={10} style={{ width: "100%" }}>
              {results.map((r, i) => (
                <div key={i} style={{ border: "1px solid #E3E7ED", borderRadius: 8, padding: "8px 12px" }}>
                  <Space size={10} style={{ marginBottom: 4 }}>
                    <Tag color={i === 0 ? "gold" : "default"}>#{i + 1}</Tag>
                    <Tag className="mono">score {r.relevance_score?.toFixed(4)}</Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>原文第 {r.index + 1} 行</Typography.Text>
                  </Space>
                  <Typography.Paragraph style={{ marginBottom: 0, whiteSpace: "pre-wrap" }}>
                    {r.document?.text ?? documents[r.index] ?? "(原文未返回，可开启 return_documents)"}
                  </Typography.Paragraph>
                </div>
              ))}
            </Space>
          ) : <Typography.Text type="secondary">等待请求…</Typography.Text>}
        <Latency out={out} />
      </OutCard>
    </Pane>
  );
}
