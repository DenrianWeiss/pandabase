# Pandabase

基于 PostgreSQL + pgvector 的 RAG 知识库系统，支持多格式文档摄取、向量检索、增量更新和多模态嵌入。

## 快速开始

### 方式一：Docker 一键启动（推荐）

这是最简单的方法。如果尚未配置 `config.yaml`，系统将启动 **配置向导**。

1.  **启动容器**：
    ```bash
    docker-compose up -d
    ```
2.  **访问配置向导**：
    在浏览器访问 `http://localhost:8080`。
3.  **完成配置**：
    输入您的 Embedding API Key 等信息。完成后系统会自动重启并进入正式环境。

### 方式二：手动开发启动

如果您想在本地运行 Go 进程：

1.  **启动基础依赖**：
    ```bash
    # 仅启动数据库和 Redis
    docker-compose up -d postgres redis
    ```
2.  **配置**：
    复制配置模板并编辑：
    ```bash
    cp config.example.yaml config.yaml
    ```
    关键配置项：
    ```yaml
    embedding:
      api_url: "https://openrouter.ai/api/v1"
      model: "qwen/qwen3-embedding-8b"
      dimensions: 4096        # ⚠️ 初始化后不可更改！
      api_key: "your-api-key"
    ```
3.  **启动服务**：
    ```bash
    CONFIG_PATH=config.yaml go run ./cmd/server
    ```


服务启动后：
- HTTP API：`http://localhost:8080`
- Web UI：`http://localhost:8080`（浏览器访问）
- MCP 接口：支持 Model Context Protocol，详见 [MCP.md](MCP.md)
- 健康检查：`GET /health`

数据库表和索引会在首次启动时自动创建，无需手动迁移。

### 4. 验证

```bash
# 健康检查
curl http://localhost:8080/health

# 运行冒烟测试
go test -v ./tests/
```

## API 使用

所有受保护的 API 需要在请求头中携带 JWT Token：

```
Authorization: Bearer <access_token>
```

### 认证

```bash
# 注册
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"password123","name":"User"}'

# 登录
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"password123"}'
```

### 命名空间

```bash
# 创建命名空间
curl -X POST http://localhost:8080/api/v1/namespaces \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-kb","description":"My knowledge base"}'

# 列出命名空间
curl http://localhost:8080/api/v1/namespaces \
  -H "Authorization: Bearer <token>"
```

### 文档管理

```bash
# 上传文档
curl -X POST http://localhost:8080/api/v1/namespaces/<ns_id>/documents \
  -H "Authorization: Bearer <token>" \
  -F "file=@document.md" \
  -F "chunk_size=500" \
  -F "chunk_overlap=50"

# 列表文档
curl http://localhost:8080/api/v1/namespaces/<ns_id>/documents \
  -H "Authorization: Bearer <token>"

# 下载文档
curl http://localhost:8080/api/v1/namespaces/<ns_id>/documents/<doc_id>/download \
  -H "Authorization: Bearer <token>" \
  -o downloaded_file

# 删除文档（级联删除分块和嵌入）
curl -X DELETE "http://localhost:8080/api/v1/namespaces/<ns_id>/documents/<doc_id>?cascade=true" \
  -H "Authorization: Bearer <token>"

# URL 导入（支持 JS 渲染）
curl -X POST http://localhost:8080/api/v1/namespaces/<ns_id>/documents/import \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/article",
    "parser_type": "web",
    "title": "Custom Title",          # 可选：手动设置标题
    "auto_extract_title": true,       # 可选：自动提取网页标题（默认true）
    "render_javascript": true,
    "render_timeout": 15,
    "wait_selector": "article",
    "render_fallback": true,
    "chunk_size": 1000,
    "chunk_overlap": 100
  }'

# 更新文档标题（仅支持 web/notion 类型）
curl -X PATCH http://localhost:8080/api/v1/namespaces/<ns_id>/documents/<doc_id>/title \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"title": "New Document Title"}'
```

支持的文档格式：
- `.txt` - 纯文本
- `.md` - Markdown
- `.pdf` - PDF 文档
- `.notion` - Notion 页面（JSON 格式：`{"url":"...", "nonce":"..."}`）
- `.html` / URL - 网页内容

### 检索

```bash
# 混合检索（默认）
curl -X POST http://localhost:8080/api/v1/search \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "搜索内容",
    "namespace_ids": ["<ns_id>"],
    "top_k": 10,
    "mode": "hybrid",
    "include_content": true
  }'

# 纯全文检索
curl -X POST http://localhost:8080/api/v1/search \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "搜索内容",
    "top_k": 10,
    "mode": "fulltext"
  }'
```

检索响应会返回命中分段邻近上下文：默认前后各 2 段并裁剪到不超过 500 字符，字段位于 `results[].context`。

网页 URL 导入时，启用 `render_javascript=true` 会先抓取脚本执行后的 DOM，再进行正文抽取、切片和向量化；若渲染失败且 `render_fallback=true`，自动降级到静态抓取。

### 网页标题管理

导入网页内容时，支持以下标题设置方式：

1. **自动提取标题**（默认）：系统从网页 HTML 中自动提取 `<title>` 标签内容
2. **手动设置标题**：通过 `title` 字段指定自定义标题，优先级高于自动提取
3. **后期修改**：通过 `PATCH /api/v1/namespaces/<ns_id>/documents/<doc_id>/title` 接口更新标题

```bash
# 导入时手动设置标题
curl -X POST http://localhost:8080/api/v1/namespaces/<ns_id>/documents/import \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/article",
    "parser_type": "web",
    "title": "My Custom Title",
    "auto_extract_title": false
  }'

# 后续修改标题
curl -X PATCH http://localhost:8080/api/v1/namespaces/<ns_id>/documents/<doc_id>/title \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"title": "Updated Title"}'
```

标题将存储在文档的 `metadata.title` 字段中，并在文档列表和搜索结果中显示。

检索模式：
| 模式 | 说明 |
|------|------|
| `vector` | 向量相似度搜索 |
| `fulltext` | 全文搜索 |
| `hybrid` | 混合检索（默认，RRF 重排序） |

### 队列管理

```bash
# 队列统计
curl http://localhost:8080/api/v1/queue/stats \
  -H "Authorization: Bearer <token>"

# 列出任务
curl "http://localhost:8080/api/v1/queue/tasks?queue=default&state=pending" \
  -H "Authorization: Bearer <token>"
```

## 配置参考

完整配置项参见 `config.example.yaml`。

| 配置 | 默认值 | 说明 |
|------|--------|------|
| `server.host` | `0.0.0.0` | 监听地址 |
| `server.port` | `8080` | 监听端口 |
| `database.host` | `localhost` | PostgreSQL 地址 |
| `database.fts_dictionary` | `simple` | 全文搜索字典（中文可用 `jieba`） |
| `embedding.dimensions` | `1536` | 向量维度（**初始化后不可更改**） |
| `embedding.api_url` | OpenAI | 兼容 API 地址 |
| `auth.jwt_secret` | 自动生成 | JWT 签名密钥 |
| `auth.enable_oauth` | `false` | 启用 OAuth |
| `storage.max_file_size` | `100` | 最大文件大小（MB） |

## Embedding 模型

### OpenRouter（推荐）

```yaml
embedding:
  api_url: "https://openrouter.ai/api/v1"
  model: "qwen/qwen3-embedding-8b"
  dimensions: 4096
  api_key: "sk-or-v1-..."
```

### OpenAI

```yaml
embedding:
  api_url: "https://api.openai.com/v1"
  model: "text-embedding-ada-002"
  dimensions: 1536
  api_key: "sk-..."
```

### 豆包多模态

```yaml
embedding:
  api_url: "https://ark.cn-beijing.volces.com/api/v3"
  model: "doubao-embedding-vision-250615"
  dimensions: 2048
  enable_multimodal: true
  api_key: "your-key"
```

> **注意**：维度超过 2000 时无法创建 HNSW 索引，将使用顺序扫描，检索速度较慢。建议优先选择 ≤ 2000 维的模型。

## 开发

```bash
# 运行单元测试
go test ./... -short

# 运行冒烟测试（需启动服务）
go test -v ./tests/

# 重置数据库
docker-compose down -v && docker-compose up -d
```

## 项目结构

```
cmd/server/         # 应用入口
internal/
  api/              # HTTP 路由和处理器
  auth/             # JWT 认证和 OAuth
  config/           # Viper 配置管理
  db/               # 数据库连接和自动迁移
  document/         # 文档生命周期服务
  embedder/         # Embedding 服务（OpenAI/豆包）
  namespace/        # 命名空间管理
  parser/           # 文档解析器（Text/MD/PDF/Notion/Web）
  chunker/          # 文本分块器
  queue/            # Asynq 异步任务队列
  retriever/        # 向量/全文/混合检索
  storage/          # 文件存储
pkg/plugin/         # 插件接口定义
web/                # Web UI（go:embed）
tests/              # 冒烟测试
```
