# 图书工具

本地优先的图书处理服务，提供两项能力：

- 图书分类：读取书籍前约 1000 字，调用 DeepSeek 推断中图法分类并归档。
- 内容修复：整理 TXT 编码、文件名、换行、论坛残留和广告内容。

支持浏览器界面、Docker 部署和命令行分类批处理。

## 架构

```
浏览器 / CLI
    │
    ├── frontend/                 Web 界面（Vite / Nginx）
    │
    └── server/                   HTTP 服务
        ├── main.go               启动、任务调度与业务接口处理
        ├── routes.go             路由注册
        ├── http_middleware.go    CORS、Token、JSON 请求校验
        │
        └── service/              CLI 与 Web 共用的业务层
            ├── classifier.go       分类与文件归档
            ├── deepseek_client.go  DeepSeek 调用
            ├── clc.go              中图法索引校验
            ├── book_reader.go      TXT/PDF/EPUB/MOBI 读取
            ├── book_repair.go      修复编排与报告
            └── text_repair.go      编码与正文清理
```

运行时数据均位于 `data/`，其中 `data/clc/` 是受版本控制的中图法索引，其余任务数据会按保留策略自动清理。

## 快速开始

### Web 开发模式

```bash
cp config.yaml.example config.yaml
# 编辑 config.yaml，填写 deepseek_api_key
./start.sh
```

- Web：<http://localhost:8887>
- API：<http://localhost:8080>

分类支持 `.txt`、`.pdf`、`.epub`、`.mobi`；修复仅支持 `.txt`。

### CLI 分类

```bash
mkdir -p source results
go run .
```

将待分类文件放进 `source/`，分类后的文件与报告写入 `results/`。

### Docker

```bash
docker compose up --build
```

Compose 会持久化分类与修复任务目录；请在启动前准备 `config.yaml`。

## 配置

从 `config.yaml.example` 复制生成配置。常用字段：

| 字段 | 说明 | 默认值 |
| --- | --- | --- |
| `deepseek_api_key` | DeepSeek API Key，分类必填 | 无 |
| `classifier_prompt_file` | 外部分类角色提示词路径 | `classifier_role.txt` |
| `clc_index_file` | 中图法索引路径 | `data/clc/clc_index.json` |
| `api_auth_token` | API Token；生产环境建议使用环境变量 | 空 |
| `allowed_origins` | 允许跨域调用的前端来源 | 本机 8887 地址 |
| `task_retention_hours` | 完成/失败任务保留时长 | `168` |
| `max_concurrent_tasks` | 同时执行的任务数 | `2` |

建议通过环境变量提供 API Token，避免将密钥写入文件：

```bash
export BOOK_DISTRIBUTE_API_TOKEN='a-long-random-token'
```

启用后，API 必须携带 `X-API-Token`。Web 页面在首次收到 401 时会提示输入，并只保存于当前浏览器的本地存储。

## 任务模型

上传创建 `pending` 任务；启动后进入 `queued` 队列，由固定数量的 worker 执行：

`pending → queued → processing → completed | failed | cancelled`

- `POST /api/cancel` 可取消排队或执行中的任务。
- DeepSeek 请求与文件循环均支持 `context` 取消。
- 状态保存在 `data/tasks.json`；服务重启后，未完成任务会标记为失败，已完成任务仍可下载。
- 默认每小时清理一次超过保留期的完成、失败或已取消任务。

## API

除 `OPTIONS` 外，所有接口在配置 Token 后都需要 `X-API-Token` 请求头。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/upload` | 上传待分类文件 |
| `POST` | `/api/classify` | 将分类任务入队 |
| `POST` | `/api/repair/upload` | 上传待修复 TXT |
| `POST` | `/api/repair` | 将修复任务入队 |
| `GET` | `/api/status/{task_id}` | 查询状态与进度 |
| `GET` | `/api/results/{task_id}` | 获取分类结果 |
| `GET` | `/api/repair/results/{task_id}` | 获取修复报告 |
| `GET` | `/api/download/{task_id}` | 下载任务 ZIP |
| `POST` | `/api/cancel` | 取消任务，JSON：`{"task_id":"..."}` |
| `POST` | `/api/clear` | 清理所有非运行中任务数据 |

上传限制：单次请求最多 100 MB、单文件最多 50 MB、最多 100 个文件。

## 开发与验证

```bash
go test ./...
go vet ./...
(cd frontend && npm ci && npm run build)
```

代码变更应至少通过 `gofmt`、`go vet` 和前端构建检查。核心业务测试位于 `service/*_test.go`。

## 运行时目录

```
data/
├── clc/                    中图法索引（保留）
├── tasks.json              任务元数据
├── uploads/<task_id>/      分类上传暂存
├── results/<task_id>/      分类结果
├── repair_uploads/<task_id>/
└── repair_results/<task_id>/
```

## 许可证

MIT
