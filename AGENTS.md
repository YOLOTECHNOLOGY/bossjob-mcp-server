# bossjob-mcp-server (`mcp-server`) Agent Guide

> **继承主仓公共规范（`YOLOTECHNOLOGY/bossjob-rd-mono`）**：`.agents/rules/global-redlines.md`、`.agents/rules/backend-common.md`、`.agents/rules/architecture-boundaries.md`、`.agents/skills/backend-service-dev/SKILL.md`。本仓项目记忆：`.agents/memory.md`。
>
> 当本文件与主仓通用约束冲突时，**以本文件（Repo 级 Override）为准**。

## 1. 本仓差异化约束 (Repo-Specific Overrides)

1. **BFF 定位**：本服务没有数据库与 Redis，严禁引入 `gorm` / `database/sql` / Redis client（缓存只用进程内）；调用其他服务**优先使用 gRPC**（`infra/grpc.go`）。缺少接口或现有接口不满足要求（字段不够、过重、有副作用、语义不符）时，先在 `api-public` 补 proto，再在对应领域服务的 `servicev3` 中实现（不在老 `service/` 新增领域逻辑），不要在本仓绕路拼装。
2. **工具契约**：MCP 工具名称、描述与参数 Schema 只来自 `api-public` `proto/open/v1/{jobseeker,recruiter}_mcp.proto` 生成的 `openv1mcp`；新增或修改工具先改 proto，不在本仓手写 `mcp.Tool`。两端共用逻辑放 `domain/mcpkit`，`domain/jobseeker` 与 `domain/recruiter` 互不引用。
3. **身份红线**：当前用户只取自已验证的 Access Token（`oauthrs.PrincipalFrom`）；工具请求不得接受 user_id / recruiter_id；输出不得包含邮箱、电话、生日、地址等联系方式。
4. **招聘者端红线**：不提供扣额度（发布 / 刷新职位、解锁人才、Talent Bomb）或主动触达求职者（打招呼、发消息、标记不合适、约面试、offer）的工具；人才数据流向第三方大模型，脱敏只在 `domain/recruiter/convert.go` 集中实现（姓名 `First L.`、不映射联系方式 / 年龄 / 性别 / 头像 / 文件链接、自由文本去邮箱电话），新增人才字段必须先评估是否需要脱敏。
5. **测试**：无 DB/Redis，`tests/` 中下游 gRPC client 通过 `dig` `Decorate` 替换为 fake，Token 用测试 RSA 密钥签发；HTTP 与工具调用走真实 `app.MCPApp` 处理器和 mcp-go 客户端。
6. **单文件行数上限**：单个 Go 文件不超过 **1000 行**。

## 2. 核心红线提醒

- Domain 层（`domain/`）承载核心逻辑，禁止跨 `domain` 引用 `entity`。
- 测试文件只放在 `tests/`（`TestCI_<method_name>`），使用 `goconvey` + `mockey`、`digutil.MustGet()` 初始化。
- push 前 `GOWORK=off go build ./...` 与 `GOWORK=off ./citest.sh` 必须通过。
