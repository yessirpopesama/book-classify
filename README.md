# 图书分类系统

这是一个基于DeepSeek API的图书自动分类工具，可以根据图书文件名自动识别并分类到对应的中国图书分类法分类号文件夹中。

## 功能特性

1. 从`source`文件夹读取每本书的**前3页**内容（.txt、.pdf、.epub 格式）
2. 使用 DeepSeek API 分析内容，识别**作者**和**国籍**
3. 使用中图法第五版给出**分类号**
4. 输出三项：**分类号**、**书籍作者**、**书籍国籍**
5. 根据分类号自动创建文件夹并移动文件
6. 分析结果汇总保存到 `结果.txt`

## 项目结构

```
book-distribute/
├── main.go              # 主程序入口
├── book_prompts.go      # 生成prompts提示字符串
├── deepseek_client.go   # DeepSeek API客户端封装
├── classifier.go        # 分类和文件移动逻辑
├── go.mod              # Go模块依赖
├── README.md           # 项目说明文档
├── source/             # 源文件目录（需要创建）
└── results/            # 分类结果目录（自动创建）
```

## 安装和使用

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置API Key

创建配置文件`config.yaml`（可以复制`config.yaml.example`）：

```bash
cp config.yaml.example config.yaml
```

然后编辑`config.yaml`，填入你的DeepSeek API密钥：

```yaml
deepseek_api_key: your_api_key_here
```

**注意**: `config.yaml`文件已被添加到`.gitignore`中，不会被提交到版本控制系统。

### 3. 准备源文件

在项目根目录创建`source`文件夹，并将要分类的图书文件放入其中：

```bash
mkdir source
# 将图书文件复制到source目录
```

### 4. 运行程序

**重要**: 必须使用以下方式之一运行，不能使用 `go run main.go`（那样只会编译单个文件）：

```bash
# 方式1: 直接运行（推荐）
go run .

# 方式2: 使用提供的脚本
./run.sh

# 方式3: 编译后运行
go build -o book-classifier .
./book-classifier
```

**注意**: 如果遇到 `undefined: LoadConfig` 等错误，说明你使用了错误的命令。请使用 `go run .` 而不是 `go run main.go`。

## 工作流程

1. **读取文件**: 程序扫描`source`目录下的所有文件（排除系统文件和特定文件）
2. **生成Prompt**: 为每个文件名生成分类提示
3. **调用API**: 使用DeepSeek API获取最佳分类号（如K837.127）
4. **创建目录**: 在`results`目录下创建对应的分类号文件夹
5. **移动文件**: 将文件移动到对应的分类文件夹中

## 分类结果

- 成功分类的文件会移动到`results/{分类号}/`目录下
- 分类失败的文件会移动到`results/uncategorized/`目录下
- 如果目标文件已存在，会自动添加序号避免覆盖

## 示例

假设有以下文件：
- `克林顿的真实生活.txt`
- `Python编程入门.txt`
- `中国历史.txt`

运行后，文件会被分类到：
- `results/K837.127/克林顿的真实生活.txt`
- `results/TP311.56/Python编程入门.txt`
- `results/K20/中国历史.txt`

## 注意事项

1. 确保创建了`config.yaml`文件并配置了正确的`deepseek_api_key`
2. 确保`source`目录存在且包含要分类的文件
3. 程序会跳过以下文件：`scan.bat`、`结果.txt`、`.DS_Store`
4. API调用可能需要一些时间，请耐心等待
5. 如果API调用失败，文件会被移动到`uncategorized`文件夹
6. `config.yaml`文件包含敏感信息，请勿提交到版本控制系统

## 依赖

- Go 1.21+
- DeepSeek API访问权限

## 许可证

MIT License
