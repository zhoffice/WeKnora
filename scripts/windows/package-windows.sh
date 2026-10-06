#!/usr/bin/env bash
set -euo pipefail

#
# Windows（Git Bash）下打包 WeKnora
#
# 用法:
#   bash scripts/windows/package-windows.sh <target> [参数]
#   scripts\windows\package.cmd <target> [参数]          # cmd / PowerShell / 双击
#
# target:
#   check          检查工具链、编译模式与 duckdb.dll
#   frontend       构建前端并同步到 web/
#   cli            构建 CLI
#   server         构建标准版（多人共享）zip，转调 go run ./scripts/windows/winpack server
#   lite           构建 Lite 单文件版 zip（SQLite + 内存队列）
#   desktop        构建桌面应用绿色版 zip（NSIS=1 时另出安装包，需要 Wails CLI）
#   images [参数]  从源码构建 Docker 镜像，参数透传给 scripts/build_images.sh
#   docker-bundle  把镜像 docker save 成离线部署目录（目标机器为 linux/amd64）
#   docreader      安装并启动文档解析服务 docreader（Python，需要 uv；默认监听 :50051）
#   run-lite       启动 dist/windows 下已打好的 Lite 包（首次自动生成 .env.lite）
#   run-desktop    启动 dist/windows 下已打好的桌面应用绿色版
#   run-server     启动 dist/windows 下已打好的标准版服务端（首次自动生成 .env）
#   run-cli [参数] 运行 dist/windows 下的 CLI，默认 --help
#   all            依次打包 cli、server、lite、desktop、images、docker-bundle，缺工具链的目标跳过
#
# 编译模式（CGO=auto|0|1，默认 auto：PATH 里有 gcc 用 cgo，否则纯 Go）:
#   纯 Go  只需要 Go + Node.js。SQLite 用 modernc、向量检索用 Go 余弦扫描、SQL 校验用
#          go-pgquery、分词用 gse，DuckDB 在运行时加载 duckdb.dll（随包分发）
#   cgo    与 Docker 镜像相同的实现（mattn SQLite + sqlite-vec、libpg_query、gojieba、duckdb-go）
#
# 环境变量:
#   CGO              编译模式，见上
#   DUCKDB_DLL       纯 Go 模式随包分发的 duckdb.dll，默认 C:/tools/libduckdb-windows-amd64/duckdb.dll
#   VERSION_TAG      产物版本号，默认 v$(cat VERSION)
#   SKIP_FRONTEND=1  复用已有 web/，不重新构建前端
#   SKIP_LICENSES=1  不附带许可证与对应源码（仅限本地自测，对外分发不要用）
#   NSIS=1           desktop 额外生成 NSIS 安装包
#   PROFILES         docker-bundle 额外带上的 compose profile，空格分隔，如 "neo4j minio"
#   WITH_SANDBOX=1   docker-bundle 一并保存沙箱镜像
#   GOPROXY / WITH_ANYDOC / NPM_REGISTRY / APT_MIRROR 等透传给 scripts/build_images.sh
#
# 产物统一输出到 dist/windows/
#

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEMPLATE_DIR="${SCRIPT_DIR}/templates"
cd "${ROOT_DIR}"

VERSION_TAG="${VERSION_TAG:-v$(tr -d '\r\n' < VERSION)}"
OUT_DIR="${ROOT_DIR}/dist/windows"
DUCKDB_DLL="${DUCKDB_DLL:-C:/tools/libduckdb-windows-amd64/duckdb.dll}"
DUCKDB_DLL_UNIX="$(cygpath -u "${DUCKDB_DLL}" 2>/dev/null || echo "${DUCKDB_DLL}")"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }
die() { log_error "$1"; exit 1; }

has() { command -v "$1" >/dev/null 2>&1; }

# 1 = cgo，0 = 纯 Go
cgo_mode() {
    case "${CGO:-auto}" in
        1 | 0) echo "${CGO}" ;;
        *) if has gcc; then echo 1; else echo 0; fi ;;
    esac
}

cgo_label() {
    if [ "$(cgo_mode)" = "1" ]; then echo "cgo"; else echo "纯 Go"; fi
}

# 工具 → 安装提示
install_hint() {
    case "$1" in
        go) echo "https://go.dev/dl/ （go.mod 要求 1.26）" ;;
        npm) echo "https://nodejs.org/ （建议 Node 22 或 24）" ;;
        gcc) echo "仅 CGO=1 需要。MSYS2: pacman -S mingw-w64-ucrt-x86_64-gcc，并把 C:/msys64/ucrt64/bin 加入 PATH" ;;
        wails) echo "可选（NSIS=1 需要）：go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0" ;;
        makensis) echo "仅 NSIS=1 需要：winget install NSIS.NSIS" ;;
        docker) echo "wsl --install 后安装 Docker Desktop" ;;
        uv) echo "docreader 需要：powershell -c \"irm https://astral.sh/uv/install.ps1 | iex\"" ;;
        *) echo "" ;;
    esac
}

require() {
    local missing=0 tool
    for tool in "$@"; do
        if ! has "${tool}"; then
            log_error "缺少 ${tool}：$(install_hint "${tool}")"
            missing=1
        fi
    done
    [ "${missing}" -eq 0 ] || exit 1
}

require_go_toolchain() {
    require go
    if [ "$(cgo_mode)" = "1" ]; then
        require gcc
    fi
}

# 打包某个目标要用到的工具
target_tools() {
    local frontend_tools="npm" cgo_tools=""
    [ "${SKIP_FRONTEND:-}" = "1" ] && frontend_tools=""
    [ "$(cgo_mode)" = "1" ] && cgo_tools="gcc"
    case "$1" in
        frontend) echo "npm" ;;
        cli) echo "go" ;;
        server) echo "go ${cgo_tools}" ;;
        lite) echo "go ${cgo_tools} ${frontend_tools}" ;;
        desktop)
            if [ "${NSIS:-}" = "1" ]; then
                echo "go ${cgo_tools} wails makensis ${frontend_tools}"
            else
                echo "go ${cgo_tools} ${frontend_tools}"
            fi
            ;;
        images | docker-bundle) echo "docker" ;;
        docreader) echo "uv" ;;
    esac
}

# 打包某个目标还缺的东西，空串表示就绪
target_missing() {
    local tool missing=()
    for tool in $(target_tools "$1"); do
        has "${tool}" || missing+=("${tool}")
    done
    if [ "$1" = "images" ] || [ "$1" = "docker-bundle" ]; then
        if has docker && ! docker info >/dev/null 2>&1; then
            missing+=("运行中的 Docker daemon")
        fi
    fi
    echo "${missing[*]}"
}

target_ready() {
    [ -z "$(target_missing "$1")" ]
}

check_env() {
    local tool version
    echo "=== 工具链 ==="
    for tool in go npm gcc wails makensis docker uv; do
        if has "${tool}"; then
            case "${tool}" in
                go) version="$(go version)" ;;
                npm) version="node $(node --version 2>/dev/null) / npm $(npm --version)" ;;
                gcc) version="$(gcc --version | head -1)" ;;
                wails) version="$(wails version 2>/dev/null | head -1)" ;;
                makensis) version="$(makensis -VERSION 2>/dev/null)" ;;
                docker) version="$(docker --version)" ;;
                uv) version="$(uv --version)" ;;
            esac
            printf "  ${GREEN}%-9s${NC} %s\n" "${tool}" "${version}"
        else
            printf "  ${YELLOW}%-9s${NC} 未安装 — %s\n" "${tool}" "$(install_hint "${tool}")"
        fi
    done
    if has docker && ! docker info >/dev/null 2>&1; then
        log_warning "docker 已安装但 daemon 未运行，请先启动 Docker Desktop"
    fi

    echo ""
    echo "=== 编译模式 ==="
    echo "  $(cgo_label)（CGO=${CGO:-auto}）"
    if [ "$(cgo_mode)" = "0" ]; then
        if [ -f "${DUCKDB_DLL_UNIX}" ]; then
            printf "  ${GREEN}duckdb.dll${NC} %s\n" "${DUCKDB_DLL}"
        else
            printf "  ${YELLOW}duckdb.dll${NC} 未找到 %s — 包内不含 DuckDB，数据分析功能不可用（设置 DUCKDB_DLL 指定路径）\n" "${DUCKDB_DLL}"
        fi
    fi

    echo ""
    echo "=== 可打包目标 ==="
    local target
    for target in cli server lite desktop images docker-bundle docreader; do
        if target_ready "${target}"; then
            printf "  ${GREEN}%-14s${NC} 就绪\n" "${target}"
        else
            printf "  ${YELLOW}%-14s${NC} 缺少: %s\n" "${target}" "$(target_missing "${target}")"
        fi
    done
}

# ── 公共步骤 ──

build_frontend() {
    if [ "${SKIP_FRONTEND:-}" = "1" ]; then
        [ -f web/index.html ] || die "SKIP_FRONTEND=1 但 web/index.html 不存在，先运行 frontend 目标"
        log_info "复用已有 web/（SKIP_FRONTEND=1）"
        return
    fi
    require npm
    log_info "构建前端..."
    export VITE_FRONTEND_COMMIT="${VITE_FRONTEND_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
    # Vite 构建这个 SPA 经常超过 Node 默认堆上限，与 frontend/Dockerfile 保持一致
    export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=4096}"
    (cd frontend && npm ci --prefer-offline && npm run build)
    rm -rf web
    cp -r frontend/dist web
    log_success "前端已同步到 web/"
}

# ldflags；EDITION=lite 决定后端是否从 ./web 提供前端（internal/router/router.go）
edition_ldflags() {
    echo "$(EDITION="$1" ./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
}

# 按编译模式设置 go build 的环境
setup_go_env() {
    export GOOS=windows GOARCH=amd64
    export CGO_ENABLED
    CGO_ENABLED="$(cgo_mode)"
    if [ "${CGO_ENABLED}" = "1" ]; then
        export CGO_CFLAGS="-Wno-deprecated-declarations"
    fi
    unset CGO_LDFLAGS
    # Milvus 与 Qdrant 的生成代码都注册了 "common.proto"，ldflags 管不到 go 子进程，用环境变量兜底
    export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn
    log_info "编译模式: $(cgo_label)"
}

copy_licenses() {
    if [ "${SKIP_LICENSES:-}" = "1" ]; then
        log_warning "SKIP_LICENSES=1：包内不含许可证与对应源码，不要对外分发"
    else
        bash ./scripts/copy-licenses.sh "$1"
    fi
}

# 纯 Go 模式下 DuckDB 来自运行时加载的 duckdb.dll，与 exe 放在同一目录
copy_duckdb_dll() {
    [ "$(cgo_mode)" = "0" ] || return 0
    if [ -f "${DUCKDB_DLL_UNIX}" ]; then
        cp "${DUCKDB_DLL_UNIX}" "$1/duckdb.dll"
    else
        log_warning "未找到 ${DUCKDB_DLL}，包内不含 duckdb.dll：CSV/Excel 数据分析不可用（设置 DUCKDB_DLL 指定路径）"
    fi
}

# Lite 与桌面版运行时都需要的文件：config、SQLite 迁移、前端、样例数据、许可证
copy_lite_runtime_files() {
    local dest="$1"
    cp -r config "${dest}/config"
    mkdir -p "${dest}/migrations/sqlite" "${dest}/dataset"
    cp -r migrations/sqlite/* "${dest}/migrations/sqlite/"
    cp -r dataset/samples "${dest}/dataset/samples"
    if [ -f web/index.html ]; then
        cp -r web "${dest}/web"
    else
        log_warning "web/index.html 不存在，包内不含前端"
    fi
    copy_duckdb_dll "${dest}"
    copy_licenses "${dest}"
}

# 用 Windows 自带的 bsdtar 打 zip（Git Bash 的 GNU tar 不支持 zip），不行再退回 PowerShell
make_zip() {
    local name="$1"
    local win_tar
    win_tar="$(cygpath -u "${SYSTEMROOT:-C:\\Windows}")/System32/tar.exe"
    (
        cd "${OUT_DIR}"
        rm -f "${name}.zip" "${name}.zip.sha256"
        if [ -x "${win_tar}" ]; then
            "${win_tar}" -a -c -f "${name}.zip" "${name}"
        else
            powershell.exe -NoProfile -Command "Compress-Archive -Path '${name}' -DestinationPath '${name}.zip' -Force"
        fi
        sha256sum "${name}.zip" > "${name}.zip.sha256"
    )
    log_success "dist/windows/${name}.zip ($(du -h "${OUT_DIR}/${name}.zip" | cut -f1))"
}

# ── 各目标 ──

build_cli() {
    require go
    local version commit date output pkg
    version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
    commit="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
    date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    pkg="github.com/Tencent/WeKnora/cli"
    output="${OUT_DIR}/weknora_${VERSION_TAG}_windows_amd64.exe"
    mkdir -p "${OUT_DIR}"

    log_info "构建 CLI..."
    (
        cd cli
        CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
            -ldflags="-s -w -X ${pkg}/internal/build.Version=${version} -X ${pkg}/internal/build.Commit=${commit} -X ${pkg}/internal/build.Date=${date}" \
            -o "${output}" .
    )
    (cd "${OUT_DIR}" && sha256sum "$(basename "${output}")" > "$(basename "${output}").sha256")
    log_success "dist/windows/$(basename "${output}")"
}

# 标准版由 Go 写的 winpack 打包，不依赖 bash：cmd / PowerShell 下直接
# go run ./scripts/windows/winpack server。CGO / DUCKDB_DLL / VERSION_TAG /
# SKIP_FRONTEND / SKIP_LICENSES 由 winpack 从环境变量读取。
build_server() {
    require_go_toolchain
    go run ./scripts/windows/winpack server
}

build_lite() {
    require_go_toolchain
    build_frontend

    local name="WeKnora-lite_${VERSION_TAG}_windows_amd64"
    local stage="${OUT_DIR}/${name}"
    rm -rf "${stage}"
    mkdir -p "${stage}"

    log_info "构建 WeKnora-lite.exe..."
    setup_go_env
    go build -tags "sqlite_fts5" -ldflags="-w -s $(edition_ldflags lite)" -o "${stage}/WeKnora-lite.exe" ./cmd/server

    log_info "组装发行目录..."
    copy_lite_runtime_files "${stage}"
    cp .env.lite.example "${stage}/"
    cp docs/LITE.md "${stage}/README.md"
    cp "${TEMPLATE_DIR}/start-lite.bat" "${stage}/"

    make_zip "${name}"
}

build_desktop() {
    require_go_toolchain
    [ "${NSIS:-}" = "1" ] && require wails makensis
    # Wails 只是壳，页面由内置的 Lite 后端从 ./web 提供，所以前端要先构建好
    build_frontend

    local name="WeKnora-Lite-App_${VERSION_TAG}_windows_amd64_portable"
    local stage="${OUT_DIR}/${name}"
    rm -rf "${stage}"
    mkdir -p "${stage}"

    setup_go_env
    local bin_dir="cmd/desktop/build/bin"
    if has wails; then
        # -s：前端已在上一步构建；-skipbindings：frontend/src/wailsjs 已入库，重新生成只会弄脏工作区
        local flags=(-clean -s -skipbindings -tags "sqlite_fts5" -platform windows/amd64 -ldflags "$(edition_ldflags lite)" -o "WeKnora Lite")
        [ "${NSIS:-}" = "1" ] && flags+=(-nsis)
        log_info "Wails 构建桌面应用..."
        (cd cmd/desktop && wails build "${flags[@]}")
        [ -f "${bin_dir}/WeKnora Lite.exe" ] || die "未找到 ${bin_dir}/WeKnora Lite.exe"
        cp "${bin_dir}/WeKnora Lite.exe" "${stage}/"
    else
        # Wails 的手动构建方式：desktop,production 标签 + windowsgui，不需要 Wails CLI（exe 不带图标）
        log_info "未安装 Wails CLI，用 go build 构建桌面应用..."
        go build -tags "desktop,production,sqlite_fts5" -ldflags="-w -s -H windowsgui $(edition_ldflags lite)" \
            -o "${stage}/WeKnora Lite.exe" ./cmd/desktop
    fi

    # 绿色版：exe 与 config、web、.env 放在同一目录，双击时工作目录就是这里，数据写在 ./data
    copy_lite_runtime_files "${stage}"
    cp .env.lite.example "${stage}/.env"
    make_zip "${name}"

    if [ "${NSIS:-}" = "1" ]; then
        local installer="${bin_dir}/WeKnora Lite-amd64-installer.exe"
        [ -f "${installer}" ] || die "未找到 ${installer}"
        local setup="WeKnora-Lite-App_${VERSION_TAG}_windows_amd64_setup.exe"
        cp "${installer}" "${OUT_DIR}/${setup}"
        (cd "${OUT_DIR}" && sha256sum "${setup}" > "${setup}.sha256")
        log_success "dist/windows/${setup}"
        log_warning "安装包只含 exe 与许可证（见 cmd/desktop/build/windows/installer/project.nsi），不含 config/web/.env，且装在 Program Files 下数据目录不可写；分发请优先用绿色版"
    fi
}

# ── 启动已打好的包（exec 替换当前 shell，GoLand 的停止按钮直接结束进程）──

# 打包目录；不存在时提示先打包
package_dir() {
    local dir="${OUT_DIR}/$1"
    [ -d "${dir}" ] || die "未找到 dist/windows/$1，先运行: bash scripts/windows/package-windows.sh $2"
    echo "${dir}"
}

# 把 KEY=VALUE 文件导入环境（去掉 CRLF，# 开头为注释）
load_env_file() {
    set -a
    # shellcheck source=/dev/null
    . <(sed 's/\r$//' "$1")
    set +a
}

run_lite() {
    local dir
    dir="$(package_dir "WeKnora-lite_${VERSION_TAG}_windows_amd64" lite)"
    cd "${dir}"
    if [ ! -f .env.lite ]; then
        cp .env.lite.example .env.lite
        log_info "已生成 ${dir}/.env.lite，可按需修改模型地址等配置"
    fi
    load_env_file .env.lite
    log_info "启动 Lite: http://localhost:8080（数据目录 ${dir}/data）"
    exec ./WeKnora-lite.exe
}

run_desktop() {
    local dir
    dir="$(package_dir "WeKnora-Lite-App_${VERSION_TAG}_windows_amd64_portable" desktop)"
    cd "${dir}"
    log_info "启动桌面应用（配置读取 ${dir}/.env，数据目录 ${dir}/data）"
    exec "./WeKnora Lite.exe"
}

# WeKnora.exe 自己读取同目录 .env（首次运行时生成）并启动包内的 qdrant / docreader
run_server() {
    local dir
    dir="$(package_dir "WeKnora-server_${VERSION_TAG}_windows_amd64" server)"
    exec "${dir}/WeKnora.exe"
}

run_cli() {
    local exe="${OUT_DIR}/weknora_${VERSION_TAG}_windows_amd64.exe"
    [ -f "${exe}" ] || die "未找到 dist/windows/$(basename "${exe}")，先运行: bash scripts/windows/package-windows.sh cli"
    if [ $# -eq 0 ]; then
        set -- --help
    fi
    exec "${exe}" "$@"
}

# docreader 在 Windows 上原生运行：依赖按 uv.lock 装进 docreader/.venv，再像容器里一样
# 从仓库根目录以 `python -m docreader.main` 启动（端口见 DOCREADER_GRPC_PORT，默认 50051）
run_docreader() {
    require uv
    log_info "同步 docreader 依赖（首次需要联网）..."
    (cd docreader && uv sync --locked --no-dev)
    log_info "启动 docreader，监听 :${DOCREADER_GRPC_PORT:-50051}（Ctrl+C 停止）"
    exec docreader/.venv/Scripts/python.exe -m docreader.main
}

build_images() {
    require docker
    docker info >/dev/null 2>&1 || die "Docker daemon 未运行，请先启动 Docker Desktop"
    bash ./scripts/build_images.sh "$@"
}

build_docker_bundle() {
    require docker
    docker info >/dev/null 2>&1 || die "Docker daemon 未运行，请先启动 Docker Desktop"

    # docker-compose.yml 的 env_file 要求 .env 存在，否则 compose 无法解析
    [ -f .env ] || cp .env.example .env

    local profile_args=() p
    for p in ${PROFILES:-}; do
        profile_args+=(--profile "${p}")
    done

    local images=()
    mapfile -t images < <(docker compose "${profile_args[@]}" config --images | tr -d '\r' | sort -u)
    if [ "${WITH_SANDBOX:-}" = "1" ]; then
        # Docker 沙箱后端默认用 :main（见 scripts/build_images.sh）
        images+=("wechatopenai/weknora-sandbox:main")
    fi
    [ "${#images[@]}" -gt 0 ] || die "docker compose config --images 没有输出镜像"

    local image
    for image in "${images[@]}"; do
        if docker image inspect "${image}" >/dev/null 2>&1; then
            continue
        fi
        case "${image}" in
            wechatopenai/weknora-* | weknora-*)
                die "本地没有 ${image}，先运行: bash scripts/windows/package-windows.sh images"
                ;;
            *)
                log_info "拉取 ${image}..."
                docker pull "${image}"
                ;;
        esac
    done

    local name="weknora-docker_${VERSION_TAG}_linux_amd64"
    local stage="${OUT_DIR}/${name}"
    rm -rf "${stage}"
    mkdir -p "${stage}/docker" "${stage}/misc"

    cp docker-compose.yml .env.example "${stage}/"
    cp -r config "${stage}/config"
    cp -r docker/searxng "${stage}/docker/searxng"
    cp misc/dex-config.yaml "${stage}/misc/"
    cp "${TEMPLATE_DIR}/docker-load.sh" "${TEMPLATE_DIR}/docker-load.bat" "${stage}/"
    printf '%s\n' "${images[@]}" > "${stage}/images.txt"
    echo "${PROFILES:-}" > "${stage}/profiles.txt"

    log_info "docker save ${#images[@]} 个镜像（体积较大，耗时几分钟）..."
    docker save -o "${stage}/images.tar" "${images[@]}"
    (cd "${stage}" && sha256sum images.tar > images.tar.sha256)

    log_success "dist/windows/${name}/ ($(du -sh "${stage}" | cut -f1))"
    log_info "整个目录拷到目标机器后运行 docker-load.sh（Linux）或 docker-load.bat（Windows）"
}

build_all() {
    local results=() target rc
    # 前端只构建一次，后面的目标复用 web/
    if target_ready lite || target_ready desktop; then
        build_frontend
        export SKIP_FRONTEND=1
    fi

    for target in cli server lite desktop images docker-bundle; do
        if ! target_ready "${target}"; then
            log_warning "跳过 ${target}：缺少 $(target_missing "${target}")"
            results+=("${target}:跳过")
            continue
        fi
        echo ""
        log_info "===== ${target} ====="
        set +e
        (
            set -e
            case "${target}" in
                cli) build_cli ;;
                server) build_server ;;
                lite) build_lite ;;
                desktop) build_desktop ;;
                images) build_images ;;
                docker-bundle) build_docker_bundle ;;
            esac
        )
        rc=$?
        set -e
        if [ "${rc}" -eq 0 ]; then
            results+=("${target}:成功")
        else
            results+=("${target}:失败")
        fi
    done

    echo ""
    echo "=== 结果（编译模式: $(cgo_label)）==="
    local failed=0 item
    for item in "${results[@]}"; do
        case "${item#*:}" in
            成功) printf "  ${GREEN}%-14s${NC} 成功\n" "${item%%:*}" ;;
            跳过) printf "  ${YELLOW}%-14s${NC} 跳过\n" "${item%%:*}" ;;
            *) printf "  ${RED}%-14s${NC} 失败\n" "${item%%:*}"; failed=1 ;;
        esac
    done
    echo "  产物目录: dist/windows/"
    return "${failed}"
}

show_help() {
    sed -n '4,44p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

TARGET="${1:-help}"
[ $# -gt 0 ] && shift
case "${TARGET}" in
    check) check_env ;;
    frontend) build_frontend ;;
    cli) build_cli ;;
    server) build_server ;;
    lite) build_lite ;;
    desktop) build_desktop ;;
    images) build_images "$@" ;;
    docker-bundle) build_docker_bundle ;;
    docreader) run_docreader ;;
    run-lite) run_lite ;;
    run-desktop) run_desktop ;;
    run-server) run_server ;;
    run-cli) run_cli "$@" ;;
    all) build_all ;;
    help | -h | --help) show_help ;;
    *)
        log_error "未知目标: ${TARGET}"
        show_help
        exit 1
        ;;
esac
