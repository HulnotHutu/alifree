#!/usr/bin/env bash
# 免费额度定时抓取包装器（供 cron 调用）。
#
# 凭证从 ~/.config/bailian-quota.env 读取（建议 chmod 600），不写死在 crontab 里：
#   ALIBABA_CLOUD_ACCESS_KEY_ID=LTAI...
#   ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
#   （可选 STS）ALIBABA_CLOUD_SECURITY_TOKEN=...
#
# 输出：
#   $OUT_DIR/free-quota-YYYY-MM-DD.json   每次运行的完整 JSON
#   $OUT_DIR/latest.json                  指向最新一次结果的软链
#   $OUT_DIR/run.log                      运行记录（含失败原因）
#
# crontab 示例（每天 9:15）：
#   15 9 * * * /home/hulut/note-space/dashscope-call/run_free_quota.sh
set -u

ENV_FILE="${BAILIAN_ENV_FILE:-$HOME/.config/bailian-quota.env}"
OUT_DIR="${BAILIAN_OUT_DIR:-$HOME/.local/share/bailian-quota}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

mkdir -p "$OUT_DIR"
OUT_FILE="$OUT_DIR/free-quota-$(date +%F).json"
TMP_FILE="$OUT_FILE.tmp"
ERR_FILE="$OUT_DIR/last-error.log"

if python3 "$SCRIPT_DIR/free_quota_lite.py" --json > "$TMP_FILE" 2> "$ERR_FILE"; then
  mv "$TMP_FILE" "$OUT_FILE"
  ln -sf "$OUT_FILE" "$OUT_DIR/latest.json"
  echo "$(date '+%F %T') ok -> $OUT_FILE" >> "$OUT_DIR/run.log"
  exit 0
else
  status=$?
  rm -f "$TMP_FILE"
  echo "$(date '+%F %T') FAILED(exit $status): $(tr '\n' ' ' < "$ERR_FILE" | head -c 300)" >> "$OUT_DIR/run.log"
  exit "$status"
fi
