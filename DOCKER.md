# Docker 编排

先准备本地配置文件：

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`，填入 `deepseek_api_key`。

启动服务：

```bash
docker compose up --build
```

访问地址：

- 前端：http://localhost:8887
- 后端：http://localhost:8080

停止服务：

```bash
docker compose down
```

## 数据目录

Compose 会把以下目录挂载到后端容器，方便保留上传与分类结果：

- `./data/uploads` -> `/app/data/uploads`
- `./data/results` -> `/app/data/results`

`data/clc` 会打包进后端镜像，用于默认的中图法索引文件 `data/clc/clc_index.json`。
