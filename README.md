# 图书分类（中图法 + DeepSeek）

基于 [DeepSeek](https://www.deepseek.com/) Chat API，读取图书正文片段，推断**中图法（第五版）分类号**、**作者**与**国籍**，并将文件归入对应目录。支持**命令行批处理**与 **Web 上传** 两种用法。

## 功能概览

- 支持的格式：`.txt`、`.pdf`、`.epub`、`.mobi`
- 从每本书抽取约**正文前 1000 字**（或等价页数）作为分析输入
- 输出：**分类号**、**书籍作者**、**书籍国籍**；结果写入 `结果.txt`
- 按分类号建文件夹并**移动**原文件；无法处理的归入 **`未识别`**
- Web：上传 → 异步分类 → 轮询状态 → 查看结果 / 下载 zip

## 目录结构

```
book-distribute/
├── main.go                 # CLI：扫描本地 source，写入 results
├── server/
│   └── main.go             # HTTP API（8080），读写 data/ 下临时与结果目录
├── frontend/               # Vite 静态页 + 代理 /api → 后端
│   ├── index.html
│   ├── main.js
│   ├── vite.config.js      # 开发服务器 5173，代理 8080
│   └── package.json
├── service/                # 核心逻辑（与 CLI / server 共用）
│   ├── config.go           # 读 config.yaml
│   ├── deepseek_client.go  # DeepSeek HTTP 客户端
│   ├── classifier.go       # ClassifyAndMove：分类、移动、写结果.txt
│   ├── book_prompts.go     # 目录与 prompt 相关
│   └── book_reader.go      # 各格式正文抽取
├── start.sh                # 启动后端 + 前端 dev（会先尝试释放 8080/5173）
├── config.yaml.example     # 配置模板（复制为 config.yaml）
├── go.mod / go.sum
├── data/                   # Web 模式使用（见下，默认已被 .gitignore）
│   ├── uploads/<task_id>/  # 上传暂存；任务分类结束后会删除该 task 目录
│   └── results/<task_id>/  # 分类结果与 结果.txt
├── source/                 # CLI 模式的待分类文件（需在本地创建）
└── results/                # CLI 模式的输出目录（需在本地创建）
```

`.gitignore` 会忽略 `config.yaml`、`data/`、`source/`、`results/` 等，避免密钥与数据入库。

## 环境要求

- **Go**：`go 1.23`（见 `go.mod`）
- **Node.js**：仅在使用 Web 前端时需要（`npm install` / `npm run dev`）
- 可用的 **DeepSeek API Key**

## 配置

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`：

| 字段 | 说明 |
|------|------|
| `deepseek_api_key` | 必填，API 密钥 |
| `deepseek_request_timeout_seconds` | 可选。单次对话请求的**总超时**（含连接与**读完响应体**）。未设置或 ≤0 时客户端默认约 **180 秒**。模型慢或网络差时可加大（如 `300`）。 |

## 使用方式

### Web（推荐）

```bash
./start.sh
```

- 前端：**http://localhost:5173**
- 后端：**http://localhost:8080**
- 前端通过 Vite 将 **`/api` 代理到后端**。

典型流程：选择文件上传 → 调用分类 → 轮询 `GET /api/status/{task_id}` → 完成后可拉取结果或下载 zip。

### CLI

1. 在项目根目录创建 `source/`，放入待分类图书。
2. 运行：

```bash
go mod tidy
go run .
```

运行前请确认已准备好 `config.yaml`（见上文「配置」）。

**注意**：请使用 `go run .`（整个模块），不要单独 `go run main.go`。

CLI 会在当前目录生成 **`目录.txt`**，并在 **`results/`** 下按分类号（及 `未识别`）移动文件，并写入 **`results/结果.txt`**。

## HTTP API（Web 后端）

| 方法 | 路径 | 作用 |
|------|------|------|
| `POST` | `/api/upload` | 多部分上传文件，返回 `task_id` |
| `POST` | `/api/classify` | body: `{"task_id":"..."}`，异步分类 |
| `GET` | `/api/status/{task_id}` | `pending` / `processing` / `completed` / `failed` |
| `GET` | `/api/download/{task_id}` | 任务完成后下载结果目录打成的 zip |
| `GET` | `/api/results/{task_id}` | 任务完成后 JSON 形式的结果行 |

CORS：`Access-Control-Allow-Origin: *`（便于本地前后端联调）。

## 结果与异常

- **成功**：文件位于 `results/<分类号>/`（Web 为 `data/results/<task_id>/<分类号>/`）。
- **失败或未识别**：`未识别` 目录；`结果.txt` 中会记录错误说明。
- **重名目标文件**：会自动加 `_1`、`_2` 等后缀，避免覆盖。
- CLI 下会跳过部分无关文件（如 `结果.txt`、`.DS_Store` 等，详见 `book_prompts.go` 中的规则）。

## 许可证

MIT License
