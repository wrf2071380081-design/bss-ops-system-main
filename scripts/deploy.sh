#!/usr/bin/env bash
# 一键发布：前后端同源同 commit，发布后自证版本一致，不一致自动回滚。
#
# 用法：bash scripts/deploy.sh [发布标签，默认 日期-rN]
#
# 为什么要有这个脚本（2026-11-20）：
#   之前靠人工判断"这次改没改前端"，合并提交的 --stat 又看不出文件，
#   结果前端长期停在旧版本，本地/线上不一致好几天没人发现。
#   现在：两端都构建、都带同一枚 commit 戳、发布后强制核对，不一致直接回滚。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="dapp@10.19.218.33"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/id_rsa}"
API_PORT=18081
WEB_PORT=8081

# 工作区必须干净：否则打进去的是未提交代码，而版本戳写的是 HEAD，
# 版本戳就成了假的（2026-11-22 实战踩到）。
if [ -n "$(git -C "$REPO_ROOT" status --porcelain)" ]; then
  echo "!! 工作区有未提交改动，先 git commit 再发布（否则版本戳不可信）"
  git -C "$REPO_ROOT" status --short | head -10
  exit 1
fi

SHA="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
BUILT_AT="$(date -Iseconds)"
TAG="${1:-$(date +%Y%m%d)-r$(git -C "$REPO_ROOT" rev-list --count HEAD)}"
# 只改一端时可跳过另一端，省一半时间：
#   SKIP_BACKEND=1 bash scripts/deploy.sh tag   只发前端
#   SKIP_FRONTEND=1 bash scripts/deploy.sh tag  只发后端
SKIP_BACKEND="${SKIP_BACKEND:-0}"
SKIP_FRONTEND="${SKIP_FRONTEND:-0}"
# 用仓库内的临时目录：Windows 的 C:// 前缀会被 GNU tar 当成远端主机名
TMP="$REPO_ROOT/.deploy-tmp"
rm -rf "$TMP"; mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT
cd "$REPO_ROOT"
cd "$REPO_ROOT"

echo "==> 发布 $TAG (commit $SHA)"

# 前后端互不依赖：并行构建，省掉前端那 15~20 秒。
# BUILD_P 默认 4：2026-11-25 实测，在**沙箱外** -p=4 编译 8.7 秒通过；
# 之前的 0xc0000409 是工具沙箱拦截 compile.exe 导致的，不是真并发问题。
# 若在受限环境里仍然崩，用 BUILD_P=1 bash scripts/deploy.sh ... 回退。
BUILD_P="${BUILD_P:-4}"
if [ "$SKIP_BACKEND" = "1" ] && [ "$SKIP_FRONTEND" = "1" ]; then
  echo "==> 1/6 前后端都跳过构建"
else
  backend_pid=""; frontend_pid=""
  if [ "$SKIP_BACKEND" != "1" ]; then
    echo "==> 1/6 构建后端（-p=${BUILD_P}，加 -s -w 减体积加快上传）"
    (
      cd "$REPO_ROOT/model-pricing-backend" || exit 1
      GOFLAGS=-p=${BUILD_P} GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=auto \
        go build -buildvcs=false \
        -ldflags "-s -w -X main.buildSHA=$SHA -X main.buildAt=$BUILT_AT" \
        -o "$TMP/server" ./cmd/server
    ) &
    backend_pid=$!
  else
    echo "==> 1/6 跳过后端构建（SKIP_BACKEND=1）"
  fi
  if [ "$SKIP_FRONTEND" != "1" ]; then
    echo "==> 2/6 构建前端（每次都建，不再判断『这次改没改前端』）"
    (
      cd "$REPO_ROOT/model-pricing-web" || exit 1
      [ -f .env.local ] || echo 'VITE_API_BASE_URL=/api' > .env.local
      npm run build >/dev/null
      rm -f dist/mockServiceWorker.js
      tar czf "$TMP/dist.tar.gz" dist
    ) &
    frontend_pid=$!
  else
    echo "==> 2/6 跳过前端构建（SKIP_FRONTEND=1）"
  fi
  [ -n "$backend_pid" ] && { wait "$backend_pid" || { echo "!! 后端构建失败"; exit 1; }; }
  [ -n "$frontend_pid" ] && { wait "$frontend_pid" || { echo "!! 前端构建失败"; exit 1; }; }
  if [ -f "$TMP/server" ]; then
    echo "    压缩后端二进制：$(du -h "$TMP/server" | cut -f1) → gzip"
    gzip -1 -c "$TMP/server" > "$TMP/server.gz"
    rm -f "$TMP/server"
  fi
fi

echo "==> 3/6 上传"
FILES=()
[ -f "$TMP/server.gz" ] && FILES+=("$TMP/server.gz")
[ -f "$TMP/dist.tar.gz" ] && FILES+=("$TMP/dist.tar.gz")
scp -i "$SSH_KEY" -o StrictHostKeyChecking=no "${FILES[@]}" "$HOST:/tmp/"

echo "==> 4/6 切换（保留旧二进制用于回滚）"
ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$HOST" bash -s <<EOF
set -e
if [ -f /tmp/server.gz ]; then
  BAK=/tmp/server.bak.\$(date +%s)
  cp /app/model_bss/bin/server \$BAK
  echo "\$BAK" > /tmp/last-good-server
  gunzip -c /tmp/server.gz > /tmp/srv.new
  chmod 755 /tmp/srv.new
  mv /tmp/srv.new /app/model_bss/bin/server
  sudo systemctl restart model-bss
fi
if [ -f /tmp/dist.tar.gz ]; then
R=/app/model_bss-web/releases/$TAG
mkdir -p \$R
tar xzf /tmp/dist.tar.gz -C \$R
find \$R -type d -exec chmod 755 {} \;
find \$R -type f -exec chmod 644 {} \;
ln -sfn \$R /app/model_bss-web/current
fi
sleep 3
EOF

echo "==> 5/6 自证：线上版本必须等于 $SHA"
ONLINE_SHA="$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$HOST" \
  "curl -s --noproxy '*' http://127.0.0.1:$API_PORT/api/version")"
echo "    后端: $ONLINE_SHA"
WEB_SHA="$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$HOST" \
  "grep -o 'name=\"build-sha\" content=\"[^\"]*\"' /app/model_bss-web/current/dist/index.html | head -1")"
echo "    前端: $WEB_SHA"

fail=0
[ "$SKIP_BACKEND" = "1" ] || echo "$ONLINE_SHA" | grep -q "\"sha\":\"$SHA\"" || { echo "!! 后端版本不符"; fail=1; }
[ "$SKIP_FRONTEND" = "1" ] || echo "$WEB_SHA" | grep -q "content=\"$SHA\"" || { echo "!! 前端版本不符"; fail=1; }

if [ "$fail" != "0" ]; then
  echo "==> 6/6 版本不一致，自动回滚后端"
  ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$HOST" bash -s <<'EOF'
set -e
BAK=$(cat /tmp/last-good-server)
cp "$BAK" /app/model_bss/bin/server
sudo systemctl restart model-bss
EOF
  echo "已回滚。请检查构建产物是否被缓存或上传失败。"
  exit 1
fi

echo "==> 6/6 通过：前后端都运行在 $SHA"
echo "    迁移提醒：本脚本不执行迁移。若本次带 migrations/，请先手工 psql -f 再发布。"
