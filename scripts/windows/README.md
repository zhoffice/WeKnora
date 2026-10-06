# Windows 本地打包与运行

只需要 **Go 1.26 + Node.js**，不需要 Docker、MinGW 或 Rust。入口是 `package-windows.sh`（Git Bash），cmd / PowerShell 下用 `package.cmd`。

```bash
bash scripts/windows/package-windows.sh check      # 工具链、编译模式、duckdb.dll
bash scripts/windows/package-windows.sh all        # cli + server + lite + desktop（有 Docker 时再加镜像）
bash scripts/windows/package-windows.sh docreader  # 安装并启动文档解析服务（需要 uv）
```

产物在 `dist/windows/`：

| 目标 | 产物 | 启动 |
|---|---|---|
| `lite` | `WeKnora-lite_*_windows_amd64.zip` | 解压后双击 `start-lite.bat`，访问 http://localhost:8080 |
| `desktop` | `WeKnora-Lite-App_*_portable.zip` | 解压后双击 `WeKnora Lite.exe`，数据在同目录 `data/` |
| `server` | `WeKnora-server_*_windows_amd64.zip` | 标准版（多人共享），见下方“标准版”一节 |
| `cli` | `weknora_*_windows_amd64.exe` | 命令行工具 |

## 标准版（多人共享）：纯 exe，不依赖 bash

打包用 Go 写的 `winpack`，在 cmd / PowerShell 里直接运行，只需要 Go、Node.js（npm）和 uv：

```bat
go run ./scripts/windows/winpack check
go run ./scripts/windows/winpack server
go run ./scripts/windows/winpack -skip-frontend -no-zip server   :: 复用 frontend/dist，只出目录
go run ./scripts/windows/winpack run-server                      :: 在当前控制台启动打好的包
```

产物 `dist/windows/WeKnora-server_*_windows_amd64/` 双击 `WeKnora.exe` 即可运行：它切到自身目录、读取同目录 `.env`（首次运行从 `.env.example` 生成并填入随机密钥后退出），再把包内服务作为子进程启动（Job Object 绑定，WeKnora 退出或崩溃时一起结束）：

| 包内目录 | 来源 | 何时启动 |
|---|---|---|
| `web/` | `npm run build` | 由 WeKnora.exe 直接提供 |
| `services/qdrant/qdrant.exe` | `-qdrant` 指定的 exe / zip，默认找 `C:\tools\qdrant\qdrant.exe`，都没有则从 GitHub 下载 `-qdrant-version` | `RETRIEVE_DRIVER` 含 qdrant 且 `QDRANT_HOST` 为本机 |
| `services/docreader/` | python.org 的嵌入式 CPython（`-python-version`，默认 3.12.10）+ 按 `docreader/uv.lock` 安装的依赖 | `DOCREADER_ADDR` 为本机 |

PostgreSQL（官方安装包，无需 pgvector / pg_search）与 Redis 6.2+ 兼容服务（如 Memurai）需在目标机器装成 Windows 服务；包内 `README.md` 是给使用者的部署说明。

`-X main.bundleLayout=windows` 由 winpack 注入，只有这样编出的 WeKnora.exe 才有上述行为，Docker 镜像与 `go run ./cmd/server` 不受影响。`package-windows.sh server` 现在转调 winpack。

## 编译模式

`CGO=auto`（默认）：PATH 里有 gcc 走 cgo，否则走纯 Go。两种模式由 build tag 切换，cgo 模式与 Docker 镜像完全一致。

| 依赖 | cgo | 纯 Go |
|---|---|---|
| SQLite（Lite 业务库） | mattn/go-sqlite3 | modernc.org/sqlite |
| 向量检索（Lite） | sqlite-vec `vec0` 虚拟表 | 普通表 + Go 注册的 `vec_distance_cosine` 全表扫描 |
| SQL 安全校验 | pg_query_go（libpg_query） | go-pgquery（同一解析器编译为 WASM） |
| 中文分词 | gojieba | gse（内置词典，结果与 gojieba 略有差异） |
| DuckDB（CSV/Excel 数据分析） | duckdb-go 静态链接 | 运行时加载 `duckdb.dll` |

纯 Go 模式注意：

- `duckdb.dll` 默认取 `C:/tools/libduckdb-windows-amd64/duckdb.dll`（用 `DUCKDB_DLL` 指定），打包时放到 exe 旁边；运行时也可用 `DUCKDB_DLL_PATH` 指定。版本保持 DuckDB 1.5.x。缺失时服务照常启动，只是数据分析不可用。
- 两种模式的 Lite 数据库**不能混用**（向量表结构不同），纯 Go 版遇到 cgo 版的库会在日志里明确报错。
- 用 mattn 建库的单元测试只能在 cgo 下运行；`retriever/sqlite`、`infrastructure/duckdb`、`agent/tools` 的 DuckDB 用例、`utils` 在纯 Go 下可直接 `CGO_ENABLED=0 go test`。

## GoLand

`.run/` 下的运行配置会被 GoLand 自动识别（该目录被 `.gitignore` 的 `.*` 忽略，共享需 `git add -f .run`）：

| 分组 | 配置 | 说明 |
|---|---|---|
| WeKnora | WeKnora Lite Server / WeKnora Desktop | 从源码编译并可断点调试（纯 Go），数据写到仓库 `./data` |
| WeKnora | WeKnora Server (remote services) | 标准版调试，在配置里把 `192.168.1.100` 改成已部署实例的地址 |
| WeKnora | Frontend Dev (Vite) | 前端开发服务器 :5173，代理到 :8080 |
| WeKnora | docreader 文档解析服务 | `package-windows.sh docreader` |
| 启动 | 启动 1~4 | 运行 `dist/windows` 下已打好的 Lite 包、桌面应用、标准版服务端、CLI（`run-*` 目标），需先打包 |
| 组合 | Lite 调试 + docreader / Lite 包 + docreader / 标准版调试（远程实例）+ 前端 | 一键同时启动多个服务 |
| Windows 打包 | 打包 0~9 | 调用上面的打包目标 |

“打包 2 - 标准版服务端”与“启动 3 - 标准版服务端包”直接运行 winpack（Go 运行配置），不经过 bash。`run-server` 首次运行会在包目录生成 `.env` 并退出，确认数据库 / Redis 密码后再启动。
