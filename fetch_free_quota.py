#!/usr/bin/env python3
"""抓取阿里云百炼（Model Studio）各模型免费额度。

原理
----
控制台「费用用量 → 免费额度」页面的数据来自内部接口（2026-10 实测）：

    POST https://bailian-cs.console.aliyun.com/data/api.json
         ?action=BroadScopeAspnGateway&product=sfm_bailian
         &api=zeldaEasy.bailian-commerce.freeTrial.queryFreeTierQuotaAsyn

两步调用：① 发起查询(modelType)拿 taskId → ② 轮询(taskId)拿 freeTierQuotas。
认证用控制台登录 Cookie（关键票据是 HttpOnly，页面 JS 读不到），因此脚本自带一个
独立的 Playwright 持久化 Profile：首次 `--login` 人工登录一次，之后 headless 复用。

用法
----
    python3 fetch_free_quota.py --login     # 首次/登录失效：打开浏览器手动登录
    python3 fetch_free_quota.py             # 抓取，JSON 输出到 stdout
    python3 fetch_free_quota.py --out quota.json
    python3 fetch_free_quota.py --type Text --type Embedding

依赖：pip install playwright && python3 -m playwright install chromium
退出码：0 成功；2 未登录（需先 --login）；1 其它错误。
"""

import argparse
import json
import sys
import time
from datetime import datetime
from pathlib import Path

from playwright.sync_api import sync_playwright

API = "zeldaEasy.bailian-commerce.freeTrial.queryFreeTierQuotaAsyn"
ENDPOINT = (
    "https://bailian-cs.console.aliyun.com/data/api.json"
    "?action=BroadScopeAspnGateway&product=sfm_bailian&api=" + API + "&_v="
)
CONSOLE_URL = (
    "https://bailian.console.aliyun.com/cn-beijing/costing-balance/free-quota"
)
CONSOLE_HOST = "bailian.console.aliyun.com"
REGION = "cn-beijing"
MODEL_TYPES = ["Text", "Vision", "Audio", "Embedding", "Multimodal"]
DEFAULT_PROFILE = Path.home() / ".cache" / "bailian-quota" / "profile"

# 在控制台页面上下文里发请求：同源 + credentials:include，Cookie 自动携带
FETCH_JS = """
async ({ endpoint, api, region, request }) => {
  const params = {
    Api: api,
    V: "1.0",
    Data: {
      queryFreeTierQuotaRequest: request,
      cornerstoneParam: {
        feTraceId: crypto.randomUUID(),
        feURL: location.href,
        protocol: "V2",
        console: "ONE_CONSOLE",
        productCode: "p_efm",
        switchAgent: 12873523,
        switchUserType: 3,
        domain: location.hostname,
        consoleSite: "BAILIAN_ALIYUN",
        userNickName: "",
        userPrincipalName: "",
        xsp_lang: "zh-CN",
        "X-Anonymous-Id": ""
      }
    }
  };
  const body = new URLSearchParams();
  body.set("params", JSON.stringify(params));
  body.set("region", region);
  const res = await fetch(endpoint, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: body.toString(),
    credentials: "include"
  });
  let json = null;
  try { json = await res.json(); } catch (e) { json = { parseError: String(e) }; }
  return { status: res.status, json };
}
"""


class LoginRequired(Exception):
    pass


def call_api(page, request_obj):
    """在页面上下文调用内部接口，返回 (inner_data, error_code)。"""
    r = page.evaluate(
        FETCH_JS,
        {"endpoint": ENDPOINT, "api": API, "region": REGION, "request": request_obj},
    )
    d = r.get("json") or {}
    outer = d.get("data") or {}
    inner = ((outer.get("DataV2") or {}).get("data") or {}).get("data") or {}
    err = outer.get("errorCode") or outer.get("errorMsg") or ""
    if "NotLogined" in str(err):
        raise LoginRequired(err)
    return inner, err


def fetch_model_type(page, model_type):
    """发起查询并按 taskId 轮询，直到返回 freeTierQuotas。"""
    inner, err = call_api(
        page,
        {"modelType": model_type, "needOverviewInfo": True, "needFreeTierOnlyStatus": True},
    )
    task_id = inner.get("taskId")
    if not task_id:
        raise RuntimeError(f"{model_type}: 未取得 taskId (err={err!r})")
    for _ in range(25):
        time.sleep(0.8)
        inner2, _ = call_api(page, {"taskId": task_id})
        if "freeTierQuotas" in inner2:
            return inner2
    raise RuntimeError(f"{model_type}: 轮询超时（taskId={task_id}）")


def wait_for_login(page, timeout_s=600):
    print("请在打开的浏览器窗口中完成登录（扫码或账号密码）。")
    print("登录完成后脚本会自动检测并保存登录态。")
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        try:
            host = page.evaluate("location.hostname")
        except Exception:
            host = None  # 页面正在跳转
        if host == CONSOLE_HOST:
            try:
                call_api(
                    page,
                    {"modelType": "Text", "needOverviewInfo": True, "needFreeTierOnlyStatus": True},
                )
                print("✓ 登录成功，登录态已保存到 Profile，下次直接运行抓取即可。")
                return 0
            except LoginRequired:
                pass
        time.sleep(4)
    print("✗ 等待登录超时，请重新运行 --login。", file=sys.stderr)
    return 2


def collect(page, types):
    models = []
    overview = {}
    for mt in types:
        data = fetch_model_type(page, mt)
        overview[mt] = data.get("freeTierQuotaOverview") or {}
        for q in data.get("freeTierQuotas") or []:
            item = dict(q)
            item["modelType"] = mt
            vp = item.get("quotaValidityPeriod")
            if isinstance(vp, (int, float)) and 0 < vp < 4_000_000_000_000:
                try:
                    item["expireAt"] = datetime.fromtimestamp(vp / 1000).astimezone().isoformat(
                        timespec="seconds"
                    )
                except (OverflowError, OSError, ValueError):
                    pass
            models.append(item)
        print(f"  {mt}: {len(data.get('freeTierQuotas') or [])} 个模型", file=sys.stderr)
    valid = [m for m in models if m.get("quotaStatus") == "VALID"]
    payload = {
        "fetched_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "source": ENDPOINT,
        "summary": {
            "totalModels": len(models),
            "withQuota": len(valid),
            "noQuota": len(models) - len(valid),
        },
        "overview": overview,
        "models": models,
    }
    return payload


def main():
    ap = argparse.ArgumentParser(description="抓取阿里云百炼免费额度")
    ap.add_argument("--login", action="store_true", help="打开可见浏览器手动登录并保存登录态")
    ap.add_argument("--out", help="输出文件路径（默认打印到 stdout）")
    ap.add_argument("--profile", default=str(DEFAULT_PROFILE), help="Playwright Profile 目录")
    ap.add_argument(
        "--type", action="append", dest="types", choices=MODEL_TYPES,
        help="只查指定模型类型（可重复，默认全部 5 种）",
    )
    ap.add_argument("--headful", action="store_true", help="抓取时也显示浏览器（调试用）")
    args = ap.parse_args()

    profile = Path(args.profile).expanduser()
    profile.parent.mkdir(parents=True, exist_ok=True)
    types = args.types or MODEL_TYPES

    with sync_playwright() as p:
        ctx = p.chromium.launch_persistent_context(
            str(profile),
            headless=not (args.login or args.headful),
            viewport={"width": 1440, "height": 900},
            args=["--disable-blink-features=AutomationControlled"],
        )
        try:
            page = ctx.pages[0] if ctx.pages else ctx.new_page()
            page.goto(CONSOLE_URL, wait_until="domcontentloaded", timeout=60_000)

            if args.login:
                return wait_for_login(page)

            print(f"抓取中（Profile: {profile}）...", file=sys.stderr)
            try:
                payload = collect(page, types)
            except LoginRequired:
                print(
                    "✗ 当前 Profile 未登录（或登录态已过期）。\n"
                    "  请先运行：python3 fetch_free_quota.py --login",
                    file=sys.stderr,
                )
                return 2
            text = json.dumps(payload, ensure_ascii=False, indent=2)
            if args.out:
                Path(args.out).write_text(text + "\n", encoding="utf-8")
                print(f"✓ 已写入 {args.out}（{payload['summary']['totalModels']} 个模型，"
                      f"其中 {payload['summary']['withQuota']} 个有额度）")
            else:
                print(text)
            return 0
        finally:
            ctx.close()


if __name__ == "__main__":
    sys.exit(main())
