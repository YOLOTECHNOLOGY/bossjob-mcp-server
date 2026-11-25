# bossjob-mcp-server

求职者 MCP 服务（第一版）：基于 [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) 的 Streamable HTTP（无状态）端点，向 ChatGPT 等 MCP 客户端暴露 `bossjob.open.v1.JobseekerMcpService` 的 10 个工具。工具定义（名称 / 描述 / JSON Schema）由 `api-public` 的 `proto/open/v1/jobseeker_mcp.proto` 生成（`openv1mcp`），本服务只实现 `JobseekerMcpServiceServer`。

## 架构

```
ChatGPT ──Bearer JWT──▶ /jobseeker/mcp (oauthrs 中间件 ➔ mcp-go StreamableHTTP)
                          └─ domain/jobseeker.Service ──gRPC(x-ctxinfo-bin)──▶ job-go / jobseeker-go / config-go
```

- 本服务是 BFF：不连数据库 / Redis，下游**只走 gRPC**（`infra/grpc.go`，`grpchelper.DefaultClientOpts` 透传 CtxInfo）。
- 依赖统一由 `dig` 管理（`di/di.go`），目录结构参考 `pay`：`cmd/`（cobra）、`conf/`、`di/`、`infra/`、`domain/`、`app/`、`tests/`。

| 工具 | 下游 gRPC |
| :--- | :--- |
| `search_jobs` | `job.v3.JobSearchService/SearchJobs`（只看远程：`SearchRemoteJobs`）；经验要求名称经 `config.v3.ConfigService/GetCountryConfig` 解析 |
| `get_job` | `job.v3.JobService/GetJobDetail` |
| `list_recommended_jobs` | `job.v3.JobSearchService/RecoJobs` |
| `get_my_profile` | `jobseeker.v3.JobseekerProfileService/GetMyProfile` + `GetProfileCompletion`、`JobseekerPreferenceService/ListJobPreferences`（后两者失败降级） |
| `evaluate_job_match` | `jobseeker.v3.JobseekerPreferenceService/JobMatchEvaluate` |
| `list_my_applications` | `job.v3.JobService/ListAppliedJobs` |
| `list_saved_jobs` / `save_job` / `unsave_job` | `job.v3.JobService/ListSavedJobs` / `CreateSavedJob` / `DeleteSavedJob`（收藏幂等） |
| `get_salary_insight` | `config.v3.ConfigSearchService/SearchJobTitleLists` + `GetCountryConfig` 解析 ➔ `job.v3.JobSearchService/SearchSalary` |

## 鉴权（OAuth 资源服务器）

- Token 由 auth-go 签发（dev issuer `https://dev.auth.bossjob.com`），RS256，公钥取自 `{issuer}/.well-known/jwks.json`（按 kid 缓存，未知 kid 按最短间隔刷新）。
- 校验：签名 + kid、`iss`、`aud = jobseeker_resource`、`exp`（必填）/`iat`/`nbf`、`scope` 含 `mcp:access`、`role = jobseeker`、`uid` 为正整数。
- 失败：401 `WWW-Authenticate: Bearer resource_metadata="<origin>/.well-known/oauth-protected-resource/jobseeker/mcp"`；缺 scope 为 403 `insufficient_scope`。
- RFC 9728 元数据：`GET /.well-known/oauth-protected-resource/jobseeker/mcp`（`authorization_servers = [issuer]`）。
- 下游 CtxInfo：`UserID = uid`、`CountryID = country_id`（缺失回退 167）、`RoleID` 由 `role` 解析、`LanguageID = 1`（英语）、`SiteID = 1`、`DeviceType = mcp`，**不传 SID**。工具参数 `country_code` 仅切换本次调用的站点。

## 配置

`-c` 指定文件，否则 `{ENV}_config.yaml`，再否则 `config.yaml`。dev 见 `development_config.yaml`，生产示例见 `config.example.yaml`。

| Key | 说明 |
| :--- | :--- |
| `map_config.oauth.issuer` | auth-go issuer（无尾斜杠 origin） |
| `map_config.oauth.jobseeker_resource` | 与 auth-go `clients[].bindings.resource` 完全一致，其 path 必须等于 `mcp.jobseeker_path` |
| `map_config.grpc.{job,jobseeker,config}` | 下游 gRPC 地址（:9000） |
| `map_config.mcp.allowed_hosts` | DNS rebinding 防护的 Host 白名单 |
| `map_config.mcp.web_base_url` | 相对 `job_url` / `company_url` 的站点根地址 |

## 运行与测试

```sh
GOWORK=off go run ./cmd serve -c development_config.yaml   # 监听 :8000，/health 探针
GOWORK=off ./citest.sh                                       # TestCI_*，下游 gRPC 通过 dig Decorate 替换为 fake
```

## 已知限制（v1）

- `list_my_applications`：下游 `JobApplicationListItem` 无公司名、投递时间与职位链接，对应字段为空。
- `search_jobs.experience_levels` 无法识别的名称会被忽略；`get_salary_insight` 无法识别职位 / 地点时返回 NotFound。
- 依赖 config-go gRPC（`GetCountryConfig` / `SearchJobTitleLists`），config-go 的 k8s Service 需暴露 9000。
