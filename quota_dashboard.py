#!/usr/bin/env python3
"""本地免费额度看板：读归档 JSON 渲染网页；凭证可用时可一键刷新。

用法：
    python3 quota_dashboard.py [--host 127.0.0.1] [--port 8377]
                               [--data-dir DIR] [--env-file FILE] [--open]

路由：
    GET  /             看板页面（同目录 quota_dashboard.html）
    GET  /api/quota    最新归档 JSON（latest.json 优先，否则按文件名取最新）
    POST /api/refresh  调 free_quota_lite.py 重新抓取并归档（需 AK/SK 凭证）

与 run_free_quota.sh 共用数据目录和归档格式：
    free-quota-YYYY-MM-DD.json / latest.json / run.log

服务默认只绑定 127.0.0.1；凭证只在服务端进程内使用，不会发给浏览器。
退出码：Ctrl+C 正常退出。
"""

import argparse
import json
import os
import subprocess
import sys
import urllib.parse
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent

DEFAULT_DATA_DIR = Path(
    os.environ.get("BAILIAN_OUT_DIR", str(Path.home() / ".local/share/bailian-quota"))
)
DEFAULT_ENV_FILE = Path(
    os.environ.get("BAILIAN_ENV_FILE", str(Path.home() / ".config/bailian-quota.env"))
)

FETCH_TIMEOUT = 240  # 秒，覆盖全量模型分页 + 额度查询


# --- 凭证 -------------------------------------------------------------------

def load_env_file(path: Path) -> dict:
    """读 KEY=VALUE 行（兼容引号与 # 注释），供刷新子进程使用。"""
    env = {}
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, value = line.partition("=")
            env[key.strip()] = value.strip().strip('"').strip("'")
    except OSError:
        pass
    return env


def has_credentials(env: dict) -> bool:
    return bool(env.get("ALIBABA_CLOUD_ACCESS_KEY_ID")) and bool(
        env.get("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
    )


def credentials_available(env_file: Path) -> bool:
    return has_credentials(load_env_file(env_file)) or has_credentials(os.environ)


# --- 数据 -------------------------------------------------------------------

def find_latest_archive(data_dir: Path) -> Path | None:
    latest = data_dir / "latest.json"
    if latest.exists():
        return latest
    archives = sorted(data_dir.glob("free-quota-*.json"))
    return archives[-1] if archives else None


def archive_snapshot(data_dir: Path, env_file: Path) -> dict:
    """读最新归档 → {ok, data?, meta, error?}。"""
    meta = {"credentials": credentials_available(env_file)}
    src = find_latest_archive(data_dir)
    if src is None:
        return {
            "ok": False,
            "error": (
                f"数据目录 {data_dir} 里没有归档 JSON。"
                "先运行 ./run_free_quota.sh 抓取一次，或在页面里配置凭证后点刷新。"
            ),
            "meta": meta,
        }
    try:
        payload = json.loads(src.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        return {"ok": False, "error": f"读取 {src} 失败：{error}", "meta": meta}

    age_hours = None
    try:
        fetched = datetime.fromisoformat(payload["fetched_at"])
        age_hours = round(
            (datetime.now(fetched.tzinfo) - fetched).total_seconds() / 3600, 1
        )
    except (KeyError, ValueError, TypeError):
        pass
    meta.update({"file": str(src), "ageHours": age_hours})
    return {"ok": True, "data": payload, "meta": meta}


def refresh_and_archive(data_dir: Path, env_file: Path) -> dict:
    """跑 free_quota_lite.py --json，成功则按 run_free_quota.sh 的约定归档。"""
    env = os.environ.copy()
    for key, value in load_env_file(env_file).items():
        if key.startswith("ALIBABA"):
            env[key] = value
    if not has_credentials(env):
        return {
            "ok": False,
            "error": (
                "缺少凭证，无法刷新。请在 "
                f"{env_file} 写入 ALIBABA_CLOUD_ACCESS_KEY_ID / "
                "ALIBABA_CLOUD_ACCESS_KEY_SECRET（见 README），重启本服务后再试。"
            ),
        }

    try:
        proc = subprocess.run(
            [sys.executable, str(SCRIPT_DIR / "free_quota_lite.py"), "--json"],
            capture_output=True,
            text=True,
            timeout=FETCH_TIMEOUT,
            cwd=SCRIPT_DIR,
            env=env,
        )
    except subprocess.TimeoutExpired:
        return {"ok": False, "error": f"刷新超时（>{FETCH_TIMEOUT}s），请稍后再试。"}

    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip().splitlines()
        return {
            "ok": False,
            "error": "抓取失败：{}".format(detail[-1][:400] if detail else proc.returncode),
        }

    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {"ok": False, "error": "抓取输出不是合法 JSON，未归档。"}

    data_dir.mkdir(parents=True, exist_ok=True)
    out_file = data_dir / f"free-quota-{datetime.now():%F}.json"
    tmp_file = out_file.with_name(out_file.name + ".tmp")
    tmp_file.write_text(proc.stdout, encoding="utf-8")
    os.replace(tmp_file, out_file)

    link_tmp = data_dir / f".latest.json.{os.getpid()}"
    if link_tmp.is_symlink() or link_tmp.exists():
        link_tmp.unlink()
    os.symlink(str(out_file), link_tmp)
    os.replace(link_tmp, data_dir / "latest.json")

    with (data_dir / "run.log").open("a", encoding="utf-8") as log:
        log.write(f"{datetime.now():%F %T} ok -> {out_file} (dashboard)\n")

    return refresh_ok_payload(data_dir, payload)


def refresh_ok_payload(data_dir: Path, payload: dict) -> dict:
    src = find_latest_archive(data_dir)
    meta = {
        "credentials": True,
        "file": str(src) if src else None,
        "ageHours": 0,
    }
    return {"ok": True, "data": payload, "meta": meta}


# --- HTTP -------------------------------------------------------------------

class Handler(BaseHTTPRequestHandler):
    server_version = "QuotaDashboard/1.0"

    @property
    def cfg(self) -> dict:
        return self.server.cfg

    def _send(self, body: bytes, content_type: str, status: int = 200):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _send_json(self, payload: dict, status: int = 200):
        self._send(
            json.dumps(payload, ensure_ascii=False).encode("utf-8"),
            "application/json; charset=utf-8",
            status,
        )

    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path
        if path == "/":
            html_path = self.cfg["html_file"]
            try:
                body = html_path.read_bytes()
            except OSError:
                self._send_json(
                    {"ok": False, "error": f"找不到页面文件 {html_path}"}, 500
                )
                return
            self._send(body, "text/html; charset=utf-8")
        elif path == "/api/quota":
            self._send_json(
                archive_snapshot(self.cfg["data_dir"], self.cfg["env_file"])
            )
        else:
            self._send_json({"ok": False, "error": "not found"}, 404)

    def do_POST(self):
        path = urllib.parse.urlparse(self.path).path
        if path == "/api/refresh":
            self._send_json(
                refresh_and_archive(self.cfg["data_dir"], self.cfg["env_file"])
            )
        else:
            self._send_json({"ok": False, "error": "not found"}, 404)

    def log_message(self, fmt, *args):  # noqa: N802 - 标准库回调
        sys.stderr.write(f"[{self.log_date_time_string()}] {fmt % args}\n")


class DashboardServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, addr, cfg: dict):
        self.cfg = cfg
        super().__init__(addr, Handler)


def main():
    parser = argparse.ArgumentParser(description="百炼免费额度本地看板")
    parser.add_argument("--host", default="127.0.0.1", help="默认只绑定本机")
    parser.add_argument("--port", type=int, default=8377)
    parser.add_argument("--data-dir", type=Path, default=DEFAULT_DATA_DIR,
                        help="归档目录（默认同 run_free_quota.sh）")
    parser.add_argument("--env-file", type=Path, default=DEFAULT_ENV_FILE,
                        help="AK/SK 凭证文件（默认同 run_free_quota.sh）")
    parser.add_argument("--open", action="store_true", help="启动后打开浏览器")
    args = parser.parse_args()

    cfg = {
        "data_dir": args.data_dir,
        "env_file": args.env_file,
        "html_file": SCRIPT_DIR / "quota_dashboard.html",
    }

    try:
        server = DashboardServer((args.host, args.port), cfg)
    except OSError as error:
        print(f"无法监听 {args.host}:{args.port}：{error}", file=sys.stderr)
        return 1

    url = f"http://{args.host}:{args.port}/"
    print(f"额度看板：{url}  （数据目录 {args.data_dir}，Ctrl+C 退出）")
    if args.open:
        import webbrowser

        webbrowser.open(url)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n已退出。")
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
