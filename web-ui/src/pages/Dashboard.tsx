import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Input,
  Progress,
  Segmented,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from "antd";
import { ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import ReactECharts from "echarts-for-react";
import { fetchHistory, fetchQuota, refreshQuota } from "../api";
import type { ModelQuota, QuotaResponse, RunInfo } from "../types";
import { TYPE_COLORS, TYPE_LABEL, fmtNum } from "../types";

type Row = ModelQuota & {
  days: number | null;
  expired: boolean;
  pct: number;
};

const QUICKS = ["可用", "即将到期", "已过期", "全部"] as const;
type Quick = (typeof QUICKS)[number];

const AXIS = { color: "#5F6B7A" };
const SPLIT = { lineStyle: { color: "#EEF1F5" } };

function expiryBucket(r: Row): string {
  if (r.days === null) return "长期/未知";
  if (r.days < 0) return "已过期";
  if (r.days <= 7) return "7天内到期";
  if (r.days <= 30) return "8–30天";
  if (r.days <= 90) return "31–90天";
  if (r.days > 730) return "长期有效";
  return "90天以上";
}

function relativeExpiry(r: Row): { text: string; color?: string } {
  if (r.days === null) return { text: "—" };
  if (r.expired) return { text: `已过期 ${-r.days} 天`, color: "#E5484D" };
  if (r.days === 0) return { text: "今天到期", color: "#E5484D" };
  if (r.days === 1) return { text: "明天到期", color: "#E5484D" };
  if (r.days <= 7) return { text: `${r.days} 天后`, color: "#E5484D" };
  if (r.days <= 30) return { text: `${r.days} 天后`, color: "#DD8E00" };
  if (r.days > 730) return { text: "长期有效", color: "#98A2B3" };
  return { text: `${r.days} 天后` };
}

export default function Dashboard() {
  const [quota, setQuota] = useState<QuotaResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [history, setHistory] = useState<RunInfo[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [quick, setQuick] = useState<Quick>("可用");
  const [type, setType] = useState<string>("全部");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  useEffect(() => {
    fetchQuota().then((out) => {
      if (!out.ok) {
        setLoadError(out.error || "加载失败");
        return;
      }
      setQuota(out);
    }).catch((e) => setLoadError(String(e)));
    fetchHistory().then(setHistory).catch(() => {});
  }, []);

  const rows: Row[] = useMemo(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    return (quota?.data?.models ?? []).map((m) => {
      let days: number | null = null;
      if (/^\d{4}-\d{2}-\d{2}$/.test(m.expires ?? "")) {
        const [y, mo, d] = m.expires.split("-").map(Number);
        days = Math.round((new Date(y, mo - 1, d).getTime() - today.getTime()) / 864e5);
      }
      return {
        ...m,
        days,
        expired: days !== null && days < 0,
        pct: m.remainingPercent ?? (m.total ? (m.remaining / m.total) * 100 : 0),
      };
    });
  }, [quota]);

  const filtered = useMemo(() => {
    const f: Record<Quick, (r: Row) => boolean> = {
      "可用": (r) => !r.expired,
      "即将到期": (r) => !r.expired && r.days !== null && r.days <= 7,
      "已过期": (r) => r.expired,
      "全部": () => true,
    };
    const q = search.toLowerCase();
    return rows.filter(
      (r) =>
        f[quick](r) &&
        (type === "全部" || (r.type || "-") === type) &&
        (!q || r.model.toLowerCase().includes(q)),
    );
  }, [rows, quick, type, search]);

  const counts = useMemo(() => {
    const f: Record<Quick, (r: Row) => boolean> = {
      "可用": (r) => !r.expired,
      "即将到期": (r) => !r.expired && r.days !== null && r.days <= 7,
      "已过期": (r) => r.expired,
      "全部": () => true,
    };
    return {
      "可用": rows.filter(f["可用"]).length,
      "即将到期": rows.filter(f["即将到期"]).length,
      "已过期": rows.filter(f["已过期"]).length,
      "全部": rows.length,
    };
  }, [rows]);

  const types = useMemo(() => {
    const by: Record<string, number> = {};
    for (const r of rows) {
      const t = r.type || "-";
      by[t] = (by[t] || 0) + 1;
    }
    return Object.keys(by).sort((a, b) => by[b] - by[a] || a.localeCompare(b));
  }, [rows]);

  function doRefresh() {
    setRefreshing(true);
    refreshQuota()
      .then((out) => {
        if (out.ok) {
          setQuota(out);
          setPage(1);
          fetchHistory().then(setHistory).catch(() => {});
        } else {
          setLoadError(out.error || "刷新失败");
        }
      })
      .catch((e) => setLoadError(String(e)))
      .finally(() => setRefreshing(false));
  }

  // --- 图表 ---
  const typeOption = useMemo(() => {
    const by: Record<string, { rem: number; n: number }> = {};
    for (const r of filtered) {
      const t = r.type || "-";
      by[t] = by[t] || { rem: 0, n: 0 };
      by[t].rem += r.remaining;
      by[t].n++;
    }
    const ts = Object.keys(by).sort((a, b) => by[b].rem - by[a].rem);
    return {
      grid: { left: 8, right: 16, top: 30, bottom: 0, containLabel: true },
      tooltip: {
        trigger: "axis" as const,
        formatter: (ps: any[]) =>
          ps.map((p) => `${p.name}：${fmtNum(p.value)}（${by[p.name].n} 个模型）`).join("<br>"),
      },
      xAxis: {
        type: "category" as const,
        data: ts,
        axisLabel: { ...AXIS, interval: 0 },
        axisLine: { lineStyle: { color: "#E3E7ED" } },
      },
      yAxis: { type: "value" as const, axisLabel: { ...AXIS, formatter: fmtNum }, splitLine: SPLIT },
      series: [
        {
          type: "bar",
          barMaxWidth: 42,
          data: ts.map((t) => ({
            value: Math.round(by[t].rem),
            itemStyle: { color: TYPE_COLORS[t] || TYPE_COLORS["-"] },
          })),
          label: { show: true, position: "top" as const, color: AXIS.color, formatter: (p: any) => fmtNum(p.value) },
        },
      ],
    };
  }, [filtered]);

  const expiryOption = useMemo(() => {
    const colors: Record<string, string> = {
      "已过期": "#E5484D", "7天内到期": "#E05200", "8–30天": "#DD8E00",
      "31–90天": "#2E90FA", "90天以上": "#12B5A2", "长期有效": "#667085", "长期/未知": "#98A2B3",
    };
    const order = ["已过期", "7天内到期", "8–30天", "31–90天", "90天以上", "长期有效", "长期/未知"];
    const counts: Record<string, number> = {};
    for (const r of filtered) counts[expiryBucket(r)] = (counts[expiryBucket(r)] || 0) + 1;
    return {
      tooltip: { trigger: "item" as const, formatter: "{b}：{c} 个（{d}%）" },
      legend: { bottom: 0, textStyle: AXIS, itemWidth: 12, itemHeight: 8 },
      series: [{
        type: "pie",
        radius: ["42%", "68%"],
        center: ["50%", "44%"],
        label: { show: true, formatter: "{b} {c}", color: AXIS.color },
        labelLine: { lineStyle: { color: "#E3E7ED" } },
        data: order.filter((k) => counts[k]).map((k) => ({
          name: k, value: counts[k], itemStyle: { color: colors[k] },
        })),
      }],
    };
  }, [filtered]);

  const ratioOption = useMemo(() => {
    const bins: [string, number, number][] = [["0–10%", 0, 10], ["10–25%", 10, 25], ["25–50%", 25, 50], ["50–75%", 50, 75], ["75–99%", 75, 100], ["100%（未使用）", 100, 101]];
    const bc = bins.map(() => 0);
    for (const r of filtered) {
      let i = bins.findIndex(([, lo, hi]) => r.pct >= lo && r.pct < hi);
      if (i < 0) i = bins.length - 1;
      bc[i]++;
    }
    const colors = ["#E5484D", "#E5484D", "#DD8E00", "#DD8E00", "#0E9F6E", "#0E9F6E"];
    return {
      grid: { left: 8, right: 16, top: 30, bottom: 0, containLabel: true },
      tooltip: { trigger: "axis" as const, formatter: (ps: any[]) => ps.map((p) => `${p.name}：${p.value} 个模型`).join("<br>") },
      xAxis: { type: "category" as const, data: bins.map((b) => b[0]), axisLabel: { ...AXIS, interval: 0 }, axisLine: { lineStyle: { color: "#E3E7ED" } } },
      yAxis: { type: "value" as const, minInterval: 1, axisLabel: AXIS, splitLine: SPLIT },
      series: [{
        type: "bar",
        barMaxWidth: 42,
        data: bc.map((v, i) => ({ value: v, itemStyle: { color: colors[i] } })),
        label: { show: true, position: "top" as const, color: AXIS.color },
      }],
    };
  }, [filtered]);

  const historyOption = useMemo(() => {
    const asc = [...history].sort((a, b) => a.id - b.id);
    if (!asc.length) return null;
    return {
      grid: { left: 8, right: 16, top: 36, bottom: 0, containLabel: true },
      tooltip: { trigger: "axis" as const },
      legend: { top: 0, textStyle: AXIS, itemWidth: 14, itemHeight: 8 },
      xAxis: {
        type: "category" as const,
        boundaryGap: false,
        data: asc.map((r) => r.fetchedAt.slice(0, 16).replace("T", " ")),
        axisLabel: { ...AXIS, hideOverlap: true },
        axisLine: { lineStyle: { color: "#E3E7ED" } },
      },
      yAxis: { type: "value" as const, axisLabel: AXIS, splitLine: SPLIT },
      series: [
        { name: "有额度模型", type: "line", smooth: true, symbolSize: 6, data: asc.map((r) => r.withQuota), itemStyle: { color: "#E05200" }, areaStyle: { color: "rgba(224,82,0,.08)" } },
        { name: "查询到的模型总数", type: "line", smooth: true, symbolSize: 6, data: asc.map((r) => r.totalModels), itemStyle: { color: "#2E90FA" }, lineStyle: { type: "dashed" } },
      ],
    };
  }, [history]);

  const columns = [
    {
      title: "模型",
      dataIndex: "model",
      key: "model",
      sorter: (a: Row, b: Row) => a.model.localeCompare(b.model),
      render: (v: string) => <span className="mono">{v}</span>,
    },
    {
      title: "类型",
      dataIndex: "type",
      key: "type",
      width: 110,
      filters: types.map((t) => ({ text: TYPE_LABEL[t] || t, value: t })),
      onFilter: (v: any, r: Row) => (r.type || "-") === v,
      render: (v: string) => (
        <Tag color={TYPE_COLORS[v] || TYPE_COLORS["-"]}>{TYPE_LABEL[v] || v}</Tag>
      ),
    },
    {
      title: "剩余",
      dataIndex: "remaining",
      key: "remaining",
      align: "right" as const,
      sorter: (a: Row, b: Row) => a.remaining - b.remaining,
      render: (v: number) => <span className="mono">{fmtNum(v)}</span>,
    },
    {
      title: "总量",
      dataIndex: "total",
      key: "total",
      align: "right" as const,
      sorter: (a: Row, b: Row) => a.total - b.total,
      render: (v: number) => <span style={{ color: "#98A2B3" }} className="mono">{fmtNum(v)}</span>,
    },
    {
      title: "剩余比例",
      dataIndex: "pct",
      key: "pct",
      sorter: (a: Row, b: Row) => a.pct - b.pct,
      width: 200,
      render: (_: any, r: Row) => (
        <Space size={10}>
          <Progress
            percent={Math.min(Math.max(r.pct, 0), 100)}
            showInfo={false}
            size={{ width: 130, height: 6 }}
            strokeColor={r.pct >= 50 ? "#0E9F6E" : r.pct >= 20 ? "#DD8E00" : "#E5484D"}
            trailColor="#E9EDF2"
          />
          <span className="mono" style={{ fontSize: 12.5, color: "#5F6B7A" }}>
            {r.pct.toFixed(1)}%
          </span>
        </Space>
      ),
    },
    {
      title: "到期",
      dataIndex: "expires",
      key: "days",
      sorter: (a: Row, b: Row) =>
        (a.expired ? 1e9 : a.days === null ? 1e8 : a.days) -
        (b.expired ? 1e9 : b.days === null ? 1e8 : b.days),
      render: (_: any, r: Row) => {
        const rel = relativeExpiry(r);
        return (
          <span style={{ whiteSpace: "nowrap" }}>
            {r.expires || "—"}
            <span style={{ marginLeft: 7, fontSize: 12.5, color: rel.color || "#5F6B7A", fontWeight: rel.color ? 550 : 400 }}>
              {rel.text}
            </span>
          </span>
        );
      },
    },
    {
      title: "用完即停",
      dataIndex: "freeTierOnly",
      key: "freetier",
      width: 110,
      sorter: (a: Row, b: Row) =>
        (a.freeTierOnly === true ? 0 : a.freeTierOnly === false ? 1 : 2) -
        (b.freeTierOnly === true ? 0 : b.freeTierOnly === false ? 1 : 2),
      render: (v?: boolean) =>
        v === true ? <Tag color="green">开启</Tag> : v === false ? <Tag style={{ color: "#98A2B3" }}>未开启</Tag> : <span style={{ color: "#98A2B3" }}>—</span>,
    },
  ];

  if (loadError && !quota) {
    return (
      <Alert
        type="warning"
        showIcon
        message="还没有可展示的额度数据"
        description={loadError}
      />
    );
  }

  const ageHours = Number(quota?.meta?.ageHours ?? NaN);
  const when = (quota?.data?.fetched_at ?? "").slice(0, 16).replace("T", " ");
  const ageLabel =
    Number.isNaN(ageHours) ? "" : ageHours < 1 ? "刚刚更新" : ageHours < 24 ? `${Math.round(ageHours)} 小时前` : `${Math.round(ageHours / 24)} 天前`;

  return (
    <Space direction="vertical" size={14} style={{ width: "100%" }}>
      {!Number.isNaN(ageHours) && ageHours >= 48 && (
        <Alert
          type="warning"
          showIcon
          message={`快照已 ${Math.round(ageHours / 24)} 天未更新，数值可能过期。配置 ~/.config/bailian-quota.env 后可点右上角刷新。`}
        />
      )}

      <Card
        styles={{ body: { padding: "14px 20px" } }}
        title="额度概览"
        extra={
          <Space>
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              快照：{when}{ageLabel && `（${ageLabel}）`} · 快照 #{quota?.meta?.snapshotId ?? "—"}
            </Typography.Text>
            <Button icon={<ReloadOutlined />} loading={refreshing} onClick={doRefresh}>
              刷新
            </Button>
          </Space>
        }
      >
        <Space size={40} wrap>
          {QUICKS.map((k) => (
            <div
              key={k}
              style={{ cursor: "pointer" }}
              onClick={() => setQuick(k)}
            >
              <Statistic
                title={k}
                value={counts[k]}
                valueStyle={{ fontWeight: quick === k ? 650 : 500,
                  color: quick === k ? "#B54100" : k === "已过期" && counts[k] > 0 ? "#E5484D" : k === "即将到期" && counts[k] > 0 ? "#DD8E00" : undefined }}
              />
            </div>
          ))}
        </Space>
      </Card>

      <Space size={10} wrap>
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索模型…"
          style={{ width: 240 }}
          value={search}
          onChange={(e) => { setSearch(e.target.value); setPage(1); }}
        />
        <Segmented
          value={quick}
          onChange={(v) => { setQuick(v as Quick); setPage(1); }}
          options={QUICKS.map((k) => ({ label: `${k} ${counts[k]}`, value: k }))}
        />
        <Select
          value={type}
          style={{ minWidth: 140 }}
          onChange={(v) => { setType(v); setPage(1); }}
          options={["全部", ...types].map((t) => ({ label: `${TYPE_LABEL[t] || t}`, value: t }))}
        />
        <Typography.Text type="secondary" style={{ marginLeft: "auto", fontSize: 13 }}>
          显示 {filtered.length} / {rows.length} 个模型
        </Typography.Text>
      </Space>

      <Table
        rowKey="model"
        size="middle"
        columns={columns as any}
        dataSource={filtered}
        pagination={{
          current: page,
          pageSize,
          showSizeChanger: true,
          pageSizeOptions: [20, 50, 100],
          onChange: (p, ps) => { setPage(p); setPageSize(ps); },
          showTotal: (t, [from, to]) => `共 ${t} 条 · ${from}–${to}`,
        }}
      />

      <Typography.Title level={4} style={{ marginTop: 8, marginBottom: 0 }}>
        可视化
      </Typography.Title>
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }}>
        <Card title="各类型剩余额度" styles={{ body: { paddingTop: 6 } }}>
          <ReactECharts option={typeOption} style={{ height: 300 }} notMerge />
        </Card>
        <Card title="到期时间分布" styles={{ body: { paddingTop: 6 } }}>
          <ReactECharts option={expiryOption} style={{ height: 300 }} notMerge />
        </Card>
        <Card title="剩余比例分布" styles={{ body: { paddingTop: 6 } }}>
          <ReactECharts option={ratioOption} style={{ height: 300 }} notMerge />
        </Card>
        <Card title="快照历史趋势">
          {historyOption ? (
            <ReactECharts option={historyOption} style={{ height: 300 }} notMerge />
          ) : (
            <Typography.Text type="secondary">暂无历史数据（挂 cron 后每日新增）</Typography.Text>
          )}
        </Card>
      </div>
      <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>
        可视化跟随上方筛选视图；「快照历史趋势」为跨快照的全局视图。数据库：{quota?.meta?.db ?? "—"}
      </Typography.Text>
    </Space>
  );
}
