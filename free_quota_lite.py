#!/usr/bin/env python3
"""百炼免费额度速查（裁减版，仅 Python 标准库，零依赖）。

只回答一件事：当前账号有哪些模型有免费额度、还剩多少、何时到期。

链路（从官方 bailian-cli v2.3.1 源码逐行裁减，见 ./cli）：
  1. 用 AK/SK 做 ACS3-HMAC-SHA256 签名，调 OpenAPI GenerateCLIAccessToken 换 CLI token
  2. 匿名调 listFoundationModels 拿模型清单（已实测：无需登录）
  3. 带 Bearer token 调 queryFreeTierQuota 拿额度，过滤出有额度的（VALID）

用法：
    export ALIBABA_CLOUD_ACCESS_KEY_ID=LTAI...
    export ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
    python3 free_quota_lite.py          # 文本表格
    python3 free_quota_lite.py --json   # JSON 输出

退出码：0 成功；1 服务/网络错误；2 缺少凭证。
"""

import argparse
import hashlib
import hmac
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone

# --- 常量（对齐 CLI：domestic + cn-beijing） --------------------------------
OPENAPI_HOST = "modelstudio.cn-beijing.aliyuncs.com"
OPENAPI_PATH = "/modelstudio/cli/generateAccessToken"
OPENAPI_ACTION = "GenerateCLIAccessToken"
OPENAPI_VERSION = "2026-02-10"

GATEWAY = "https://bailian-cs.console.aliyun.com/cli/api.json"
GATEWAY_ACTION = "BroadScopeAspnGateway"
GATEWAY_PRODUCT = "sfm_bailian"
REGION = "cn-beijing"

MODEL_LIST_API = "zeldaHttp.dashscopeModel./zelda/api/v1/modelCenter/listFoundationModels"
FREE_TIER_API = "zeldaEasy.bailian-commerce.freeTrial.queryFreeTierQuota"
PAGE_SIZE = 50

# capabilities → 展示类型（对齐 cli 的 usage/shared.ts）
CAPABILITY_TO_TYPE = {
    "Reasoning": "Text", "TG": "Text", "VU": "Text",
    "IG": "Vision", "VG": "Vision",
    "Realtime-Omni": "Multimodal", "Multimodal-Omni": "Multimodal",
    "ASR": "Audio", "TTS": "Audio", "Voice-Replication": "Audio",
    "Realtime-Text-to-Speech": "Audio", "Realtime-Voice-Replication": "Audio",
    "Realtime-ASR": "Audio", "Realtime-Audio-Translate": "Audio",
    "ME": "Embedding", "TR": "Embedding",
}


# --- HTTP -------------------------------------------------------------------

def http_post(url, headers, body):
    req = urllib.request.Request(url, data=body, method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.read().decode("utf-8")
    except urllib.error.HTTPError as error:
        # OpenAPI 的错误也走 HTTP 状态码（如未知 AK 返回 404 + JSON），透出响应体
        detail = ""
        try:
            detail = error.read().decode("utf-8", "replace")[:500]
        except Exception:  # noqa: BLE001
            pass
        raise RuntimeError(f"HTTP {error.code}: {detail or error.reason}") from error


# --- ACS3 签名（逐行对齐 cli/packages/core/src/client/acs.ts） ---------------

def _sha256_hex(data: str) -> str:
    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def sign_acs_request(ak: str, sk: str, sts_token: str | None) -> dict:
    date_iso = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    body = ""  # GenerateCLIAccessToken 无请求体
    hashed_body = _sha256_hex(body)

    headers = {
        "host": OPENAPI_HOST,
        "x-acs-action": OPENAPI_ACTION,
        "x-acs-version": OPENAPI_VERSION,
        "x-acs-date": date_iso,
        "x-acs-signature-nonce": str(uuid.uuid4()),
        "x-acs-content-sha256": hashed_body,
        "content-type": "application/json",
    }
    if sts_token:
        headers["x-acs-security-token"] = sts_token

    signed_keys = sorted(
        k for k in headers
        if k == "host" or k == "content-type" or k.startswith("x-acs-")
    )
    canonical_headers = "".join(f"{k}:{headers[k]}\n" for k in signed_keys)
    signed_headers = ";".join(signed_keys)
    canonical_request = "\n".join(
        ["POST", OPENAPI_PATH, "", canonical_headers, signed_headers, hashed_body]
    )
    string_to_sign = "ACS3-HMAC-SHA256\n" + _sha256_hex(canonical_request)
    signature = hmac.new(
        sk.encode("utf-8"), string_to_sign.encode("utf-8"), hashlib.sha256
    ).hexdigest()

    out = dict(headers)
    out["Authorization"] = (
        f"ACS3-HMAC-SHA256 Credential={ak},"
        f"SignedHeaders={signed_headers},Signature={signature}"
    )
    # CLI 附加的渠道标识头（非签名头，对齐 client.ts + headers.ts）
    out["x-dashscope-source-config"] = (
        '{"channel":"bailian-cli","tags":{"t1":"public","t2":"bl","t3":"2.3.1"}}'
    )
    out["x-dashscope-openapisource"] = "BailianCLI"
    out["X-Dashscope-Service"] = "bailian-cli"
    return out


def generate_cli_token(ak: str, sk: str, sts_token: str | None) -> str:
    """AK/SK → CLI access token（OpenAPI GenerateCLIAccessToken）。"""
    url = f"https://{OPENAPI_HOST}{OPENAPI_PATH}"
    headers = sign_acs_request(ak, sk, sts_token)
    raw = http_post(url, headers, None)
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        raise RuntimeError(f"OpenAPI 返回非 JSON：{raw[:300]}")
    if data.get("Success") is False:
        raise RuntimeError(
            f"GenerateCLIAccessToken 失败：{data.get('Code')} - {data.get('Message')}"
        )
    token = data.get("cliAccessToken")
    if not token:
        raise RuntimeError(f"响应中没有 cliAccessToken：{json.dumps(data)[:300]}")
    return token


# --- Console 网关（/cli/api.json） ------------------------------------------

def gateway_call(api: str, data: dict, token: str | None):
    params = json.dumps(
        {
            "Api": api,
            "V": "1.0",
            "Data": {
                **data,
                "cornerstoneParam": {
                    "protocol": "V2",
                    "console": "ONE_CONSOLE",
                    "productCode": "p_efm",
                    "switchUserType": 3,
                    "consoleSite": "BAILIAN_ALIYUN",
                },
            },
        },
        ensure_ascii=False,
        separators=(",", ":"),
    )
    url = (
        f"{GATEWAY}?action={GATEWAY_ACTION}&product={GATEWAY_PRODUCT}"
        f"&api={urllib.parse.quote(api, safe='')}"
    )
    body = urllib.parse.urlencode({"params": params, "region": REGION}).encode()
    headers = {
        "Accept": "*/*",
        "Content-Type": "application/x-www-form-urlencoded",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    result = json.loads(http_post(url, headers, body))
    inner = result.get("data") or {}
    if inner.get("success") is False and inner.get("errorCode"):
        raise RuntimeError(f"网关错误：{inner.get('errorCode')}")
    return result


def unwrap_gateway(result: dict) -> dict:
    """剥掉 data.DataV2.data.data 双层信封（对齐 cli 的 unwrapResponse）。"""
    data = result.get("data") or {}
    data_v2 = data.get("DataV2")
    if data_v2:
        inner = data_v2.get("data") or {}
        return inner.get("data") or inner or data_v2
    return data.get("data") or data


# --- 业务 --------------------------------------------------------------------

def resolve_model_type(capabilities):
    for capability in capabilities or []:
        model_type = CAPABILITY_TO_TYPE.get(capability)
        if model_type:
            return model_type
    return "-"


def fetch_all_models():
    """全部模型 → {name: type}；该接口匿名可用（已实测）。"""
    models = {}
    page_no = 1
    while True:
        result = gateway_call(
            MODEL_LIST_API,
            {
                "input": {
                    "pageNo": page_no, "pageSize": PAGE_SIZE, "name": "",
                    "providers": [], "inferenceProviders": [], "features": [],
                    "group": True, "capabilities": [], "contextWindows": [],
                }
            },
            token=None,
        )
        data = unwrap_gateway(result)
        total = data.get("total") or 0
        found = 0
        for group in data.get("list") or []:
            items = group.get("items") or [group]
            for item in items:
                name = item.get("model")
                if name:
                    models[name] = resolve_model_type(item.get("capabilities"))
                    found += 1
        if found == 0 or page_no * PAGE_SIZE >= total:
            break
        page_no += 1
    return models


def fetch_free_quotas(token: str, model_names: list[str]):
    result = gateway_call(
        FREE_TIER_API,
        {"queryFreeTierQuotaRequest": {"models": model_names}},
        token=token,
    )
    data = unwrap_gateway(result)
    return data.get("freeTierQuotas") or []


def format_number(value) -> str:
    value = int(value)  # 接口返回可能是整数值浮点（如 999668.0）
    if value >= 1_000_000 and value % 100_000 == 0:
        return f"{value // 1_000_000}M"
    if value >= 1_000:
        text = f"{value / 1_000:.1f}".rstrip("0").rstrip(".")
        return f"{text}K"
    return str(value)


def format_date(ms: int) -> str:
    if not ms:
        return "-"
    try:
        return datetime.fromtimestamp(ms / 1000).astimezone().strftime("%Y-%m-%d")
    except (OverflowError, OSError, ValueError):
        return "-"


def main():
    parser = argparse.ArgumentParser(description="百炼免费额度速查（裁减版）")
    parser.add_argument("--json", action="store_true", help="输出 JSON")
    parser.add_argument("--ak", help="AccessKey ID（默认读环境变量 ALIBABA_CLOUD_ACCESS_KEY_ID）")
    parser.add_argument("--sk", help="AccessKey Secret（默认读环境变量 ALIBABA_CLOUD_ACCESS_KEY_SECRET）")
    args = parser.parse_args()

    ak = args.ak or os.environ.get("ALIBABA_CLOUD_ACCESS_KEY_ID", "").strip()
    sk = args.sk or os.environ.get("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "").strip()
    sts_token = os.environ.get("ALIBABA_CLOUD_SECURITY_TOKEN", "").strip() or None
    if not ak or not sk:
        print(
            "缺少凭证：请设置 ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET 环境变量"
            "（或传 --ak/--sk）。",
            file=sys.stderr,
        )
        return 2

    try:
        token = generate_cli_token(ak, sk, sts_token)
        model_types = fetch_all_models()
        quotas = fetch_free_quotas(token, list(model_types))
    except Exception as error:  # noqa: BLE001 - 服务端错误原样透传
        print(f"错误：{error}", file=sys.stderr)
        return 1

    # 与 CLI 一致的过滤/排序：有额度 = VALID 且总额度 > 0；按剩余百分比升序（最紧张在前）
    with_quota = [
        quota for quota in quotas
        if quota.get("quotaStatus") == "VALID" and (quota.get("quotaInitTotal") or 0) > 0
    ]
    with_quota.sort(
        key=lambda quota: (quota.get("quotaTotal") or 0) / (quota.get("quotaInitTotal") or 1)
    )

    items = []
    for quota in with_quota:
        init_total = quota.get("quotaInitTotal") or 0
        remaining = quota.get("quotaTotal") or 0
        items.append({
            "model": quota.get("model"),
            "type": model_types.get(quota.get("model"), "-"),
            "remaining": remaining,
            "total": init_total,
            "remainingPercent": round(remaining / init_total * 100, 1) if init_total else None,
            "expires": format_date(quota.get("quotaValidityPeriod") or 0),
        })

    if args.json:
        payload = {
            "fetched_at": datetime.now().astimezone().isoformat(timespec="seconds"),
            "source": f"{GATEWAY}?action={GATEWAY_ACTION}&product={GATEWAY_PRODUCT}&api={FREE_TIER_API}",
            "summary": {
                "totalModels": len(model_types),
                "withQuota": len(items),
            },
            "models": items,
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return 0

    if not items:
        print(f"没有模型有可用免费额度（共查询 {len(model_types)} 个模型）。")
        return 0

    header = f"免费额度：{len(items)} 个模型有额度（共 {len(model_types)} 个模型）"
    print(header)
    print(f"{'model':<36}{'type':<12}{'remaining':>12}{'total':>10}{'%':>8}  expires")
    for item in items:
        print(
            f"{item['model']:<36}{item['type']:<12}"
            f"{format_number(item['remaining']):>12}{format_number(item['total']):>10}"
            f"{item['remainingPercent']:>8}  {item['expires']}"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
