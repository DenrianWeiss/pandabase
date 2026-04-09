# Pandabase - RAG 知识库系统实现计划

## 项目概述

Pandabase 是一个基于 PostgreSQL + pgvector 的文档检索系统（RAG），支持多格式文档摄取、向量检索、增量更新和多模态嵌入。系统针对高性能检索进行了优化，支持半精度向量索引和自动化部署向导。

## 已实现功能

### ✅ 第一阶段：基础架构（已完成）

#### 1.1 项目结构
```
/Users/admin/Workspace/pandabase/
├── cmd/server/                    # 应用入口 (支持初始化向导模式)
├── internal/
│   ├── api/                       # HTTP API 路由与处理器
│   ├── auth/                      # JWT 认证与 OAuth 集成
│   ├── config/                    # Viper 配置系统 (支持 YAML 序列化)
│   ├── db/                        # 数据库连接、动态迁移与维度验证
│   ├── document/                  # 文档生命周期服务
│   ├── embedder/                  # Embedding 服务 (支持维度限制参数)
│   ├── namespace/                 # 命名空间管理
│   ├── parser/                    # 多格式文档解析器 (Text/MD/PDF/Notion/Web)
│   ├── chunker/                   # 文本分块策略
│   ├── queue/                     # Asynq 任务队列
│   ├── setup/                     # 网页初始化向导逻辑
│   ├── storage/                   # 文件存储 (本地/内存)
│   └── retriever/                 # 检索服务 (支持 Vector/FT/Hybrid)
├── pkg/plugin/                   # 插件接口定义
├── web/                           # 嵌入式 Web 管理后台
├── Dockerfile                     # 多阶段镜像构建文件
├── docker-compose.yml             # 全栈容器编排 (App + DB + Redis)
├── config.example.yaml            # 配置示例
└── .env.example                   # 环境变量示例
```

#### 1.2 核心特性
- ✅ **动态向量库校验**：启动时检测维度并自动匹配数据库设置，不一致则安全退出。
- ✅ **智能索引策略**：
  - `< 2000 维`：自动建立标准 HNSW 索引。
  - `2000 - 4000 维`：支持 `halfvec` (半精度)，并建议用户开启以支持 HNSW 索引。
  - `> 4000 维`：自动切换至顺序扫描并发出性能警告（pgvector 限制）。
- ✅ **自动化部署**：集成 Dockerfile 与 Docker Compose，支持一键启动。
- ✅ **初始化向导**：容器首次启动若无配置，将自动开启浏览器端的配置向导（Setup Wizard）。
- ✅ **首位管理员注册 (Onboarding)**：系统初次运行且数据库为空时，自动引导至管理员创建页面；创建后公共注册入口立即锁定。
- ✅ **多租户隔离**：基于 Namespace 的数据物理隔离。

### ✅ 第四阶段：用户管理与权限（已完成）
- ✅ **角色访问控制 (RBAC)**：支持 Admin (系统管理)、User (读写)、Viewer (只读) 三种角色。
- ✅ **管理端 API**：提供管理员专属的 `/api/v1/users` 接口，支持查看、修改及删除用户。
- ✅ **管理后台 UI**：新增用户管理视图，允许管理员直接创建新用户或调整现有用户的权限等级。
- ✅ **响应式 UI 升级**：深度参考 Docurus 设计语言，采用纯白背景、翡翠绿 (Emerald) 主体色调，提升操作直观性。

### ✅ 第二阶段：文档处理（已完成）
- ✅ **多格式支持**：
  - **Text/Markdown**: 原生解析，支持结构化提取。
  - **PDF**: 基于 `ledongthuc/pdf` 的文本提取。
  - **Web**: 基于 `go-readability` 的网页正文抓取。
  - **Notion**: 通过 Notion API 直接摄取页面内容。
- ✅ **分块策略**：支持行分块、Markdown 标题分块及结构化分块，可配置重叠度。

### ✅ 第三阶段：Embedding 与检索（已完成）
- ✅ **标准化 API**：兼容 OpenAI 接口，支持 OpenRouter (qwen3) 和豆包多模态。
- ✅ **维度限制**：在 API 请求层级支持通过 `dimensions` 参数限制模型输出维度。
- ✅ **混合检索**：结合向量搜索（pgvector）与全文搜索（TSVector），通过 RRF 算法进行结果重排序。

### ✅ 第六阶段：异步任务与管理（已完成）
- ✅ **高可靠队列**：基于 Redis + Asynq 的异步处理链路，支持重试与优先级。
- ✅ **生命周期管理**：支持增量更新（内容哈希检测）、级联删除及异步清理。

---

## 技术栈汇总

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.25+ |
| 数据库 | PostgreSQL 17 + pgvector 0.8.0+ |
| 缓存/队列 | Redis 7 + Asynq |
| 应用框架 | Gin |
| ORM | GORM |
| 解析库 | goldmark, pdf, readability, notionapi |
| 部署 | Docker, Docker Compose, Alpine |

---

## API 端点概览

### 1. 基础
- `GET /health`: 健康检查
- `GET /setup`: 初始化向导页面

### 2. 认证与用户
- `GET /api/v1/auth/status`: 检查系统初始化状态 (是否已创建管理员)
- `POST /api/v1/auth/register`: 初始管理员注册 (仅在系统未初始化时开放)
- `POST /api/v1/auth/login`: 登录并获取 JWT
- `GET /api/v1/auth/me`: 获取当前用户信息
- `GET/POST /api/v1/users`: 用户管理 (管理员专属)
- `PUT/DELETE /api/v1/users/:id`: 更新或删除用户 (管理员专属)

### 3. 数据管理
- `GET/POST /api/v1/namespaces`: 命名空间管理
- `POST /api/v1/namespaces/:ns_id/documents`: 上传文档 (通过 :ns_id 避免路由冲突)
- `POST /api/v1/search`: 混合/向量/全文检索

---

## 优化建议 (Roadmap)

3. **GORM 嵌套结构体关联解析**：目前部分复杂 JSON 采用手动映射。
4. **日志系统整合**：目前 `internal/logger` 包与 `main.go` 内部日志逻辑尚未完全解耦。

---

## 参考资料

- `README.md`: 使用手册与快速开始。
- `Dockerfile`: 镜像构建逻辑。
- `docker-compose.yml`: 服务依赖关系。
