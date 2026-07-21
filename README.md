# 图书工具

面向本地图书处理的 Web 工具集，当前包含：

- **图书分类**：基于 [DeepSeek](https://www.deepseek.com/) Chat API，推断中图法分类号并归档
- **内容修复**：上传 txt，优化书名、修复编码/格式、对齐正文并剔除垃圾内容

支持**命令行批处理**（分类）与 **Web 上传** 两种用法。

## 功能概览

### 图书分类

- 支持的格式：`.txt`、`.pdf`、`.epub`、`.mobi`
- 从每本书抽取约**正文前 1000 字**作为分析输入
- 输出：**分类号**、**最优分类路径**、**书籍作者**、**书籍国籍**
- 与本地 `data/clc/clc_index.json` 校验路径一致 → `结果.txt` + 按分类号归档
- 校验未通过 → `待进行分类.txt` + 图书移入 `待分类/`

### 内容修复

- 仅支持 `.txt`
- **书名优化**：去除广告站点、非法文件名字符、多余空白与 emoji
- **编码修复**：自动识别 UTF-8（含 BOM）、UTF-16、GBK/GB18030，统一输出 UTF-8
- **格式修复**：归一化换行符、去除 NUL/控制字符、剔除 HTML 标签
- **正文整理**：对齐空格、剔除广告/水印行、压缩多余空行
- 输出修复后的 txt 与 `修复报告.txt`

## 目录结构

```
book-distribute/
├── main.go                 # CLI：扫描本地 source，写入 results
├── server/
│   └── main.go             # HTTP API（8080）
├── frontend/               # Vite 静态页 + 代理 /api → 后端
│   ├── index.html
│   ├── main.js
│   └── vite.config.js      # 开发服务器 8887
├── service/                # 核心逻辑（CLI / server 共用）
│   ├── classifier.go       # 图书分类
│   ├── book_repair.go      # 内容修复编排
│   ├── text_repair.go      # 编码与正文修复
│   ├── book_filename.go    # 书名优化
│   └── ...
├── start.sh                # 启动后端 + 前端 dev
├── config.yaml.example
└── data/
    ├── uploads/<task_id>/       # 分类上传暂存
    ├── results/<task_id>/       # 分类结果
    ├── repair_uploads/<task_id>/ # 修复上传暂存
    └── repair_results/<task_id>/ # 修复结果
```

## 环境要求

- **Go**：`go 1.23`
- **Node.js**：Web 前端开发时需要
- **DeepSeek API Key**：仅分类功能需要

## 配置

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml` 填入 `deepseek_api_key`（分类功能必填）。

## 使用方式

### Web（推荐）

```bash
./start.sh
```

- 前端：**http://localhost:8887**
- 后端：**http://localhost:8080**

在页面顶部切换 **图书分类** / **内容修复**，上传文件后等待处理完成，可查看结果并下载 zip。

### CLI（仅分类）

```bash
mkdir -p source results
go run .
```

将待分类图书放入 `source/`，结果输出到 `results/`。

## HTTP API

### 图书分类

| 方法 | 路径 | 作用 |
|------|------|------|
| `POST` | `/api/upload` | 上传书籍，返回 `task_id` |
| `POST` | `/api/classify` | 启动异步分类 |
| `GET` | `/api/results/{task_id}` | 分类结果 JSON |
| `GET` | `/api/status/{task_id}` | 任务状态 |
| `GET` | `/api/download/{task_id}` | 下载结果 zip |

### 内容修复

| 方法 | 路径 | 作用 |
|------|------|------|
| `POST` | `/api/repair/upload` | 上传 txt，返回 `task_id` |
| `POST` | `/api/repair` | 启动异步修复 |
| `GET` | `/api/repair/results/{task_id}` | 修复报告 JSON |
| `GET` | `/api/status/{task_id}` | 任务状态（与分类共用） |
| `GET` | `/api/download/{task_id}` | 下载修复 zip |

## 许可证

MIT License
