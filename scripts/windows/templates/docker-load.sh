#!/usr/bin/env bash
set -euo pipefail

# 在目标机器上导入离线镜像并启动 WeKnora（由 scripts/windows/package-windows.sh docker-bundle 生成）
cd "$(dirname "$0")"

if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c images.tar.sha256
fi

echo ">> docker load..."
docker load -i images.tar

if [ ! -f .env ]; then
    cp .env.example .env
    echo ">> 已从 .env.example 生成 .env，生产环境请修改其中的密钥与密码后再启动"
fi

# 打包时选的 profile，转成 compose 认识的逗号分隔形式
profiles="$(tr -d '\r' < profiles.txt | xargs)"
if [ -n "${profiles}" ]; then
    export COMPOSE_PROFILES="${profiles// /,}"
    echo ">> COMPOSE_PROFILES=${COMPOSE_PROFILES}"
fi

# 镜像都在本地了，禁止构建（包里没有源码）
docker compose up -d --no-build
echo ">> 启动完成：Web UI http://<本机IP>  后端 API :8080"
