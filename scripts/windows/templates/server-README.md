# WeKnora 标准版（多人共享）· Windows

双击 `WeKnora.exe` 即可启动，不需要 Docker、bash、Python 或 MinGW。包内自带：

| 目录 | 内容 | 何时自动启动 |
|---|---|---|
| `services\qdrant` | Qdrant 向量库 | `.env` 中 `RETRIEVE_DRIVER=qdrant` 且 `QDRANT_HOST` 是本机 |
| `services\docreader` | 文档解析服务（内嵌 Python） | `DOCREADER_ADDR` 是本机 |
| `web` | 前端页面 | 随 WeKnora.exe 一起提供 |

这两个服务是 WeKnora.exe 的子进程，关闭 WeKnora 窗口时会一起退出。

## 一、先装两个 Windows 服务（只需一次）

1. **PostgreSQL**：官方 Windows 安装包（EDB，16 或 17 版均可），记下 `postgres` 用户的密码，再用 pgAdmin 或 `psql` 建一个名为 `WeKnora` 的库。无需安装 pgvector / pg_search。
2. **Redis 6.2+ 兼容服务**：推荐 Memurai（https://www.memurai.com/ ，原生 Windows，兼容 Redis 7）。老的 Redis for Windows 5.x 缺少 `GETDEL` 等命令，不能用。

两者安装后都作为 Windows 服务开机自启。

## 二、首次启动

1. 双击 `WeKnora.exe`：自动生成 `.env` 并填入随机密钥，然后提示退出。
2. 打开 `.env`，确认 `DB_PASSWORD`、`REDIS_PASSWORD` 与上一步安装时一致。
3. 再次双击 `WeKnora.exe`，看到 `Server is running at 0.0.0.0:8080` 即启动成功，浏览器访问 http://localhost:8080 。

`.env` 里的 `JWT_SECRET` / `SYSTEM_AES_KEY` / `SYSTEM_SIGNING_KEY` 生成后不要再改，也不要删掉 `.env` 重新生成。

## 三、让其他人访问

- 其他人用 `http://<本机局域网IP>:8080` 访问，各自注册账号，再通过共享空间协作。
- 首次运行时 Windows 防火墙会询问是否允许 `WeKnora.exe` 联网，勾选“专用网络”并允许。
- 防火墙询问 `python.exe`（docreader）时选择“取消”即可，它只需要本机访问。
- 所有人注册完后，可在 `.env` 设置 `DISABLE_REGISTRATION=true` 关闭自助注册。

## 数据与日志

| 路径 | 内容 |
|---|---|
| PostgreSQL `WeKnora` 库 | 账号、知识库、对话等业务数据 |
| `data\qdrant` | 向量索引 |
| `data\files` | 上传的原始文件 |
| `logs\weknora.log`、`logs\qdrant.log`、`logs\docreader.log` | 各服务日志 |

备份时同时备份 PostgreSQL、`data\` 与 `.env`。升级时解压新版本，把旧目录的 `.env` 与 `data\` 拷过去即可。

## 常见问题

- **窗口一闪而过 / 提示启动失败**：窗口会停住显示错误，常见原因是 PostgreSQL / Redis 未启动或密码不对。
- **8080 端口被占用**：修改 `config\config.yaml` 中 `server.port`。
- **改用自己部署的 Qdrant / docreader**：把 `.env` 里 `QDRANT_HOST` / `DOCREADER_ADDR` 改成对方地址，包内对应服务就不会启动；或设置 `WEKNORA_BUNDLED_SERVICES=off`。
- **旧版 Word（.doc）解析**：需要本机安装 LibreOffice；`.docx`、PDF、Excel、PPT 等不需要。
