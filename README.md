# bossjob-mcp-server

Bossjob MCP 服务（第一版）：基于 [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) 的两个 Streamable HTTP（无状态）端点，向 ChatGPT / Claude 等 MCP 客户端暴露：

- `/jobseeker/mcp`：`bossjob.open.v1.JobseekerMcpService` 的 10 个求职者工具；
- `/recruiter/mcp`：`bossjob.open.v1.RecruiterMcpService` 的 13 个招聘者工具。

工具定义（名称 / 描述 / JSON Schema）由 `api-public` 的 `proto/open/v1/{jobseeker,recruiter}_mcp.proto` 生成（`openv1mcp`），本服务只实现对应的 `*McpServiceServer`。

## 架构

```
MCP Client ──Bearer JWT──▶ /jobseeker/mcp (oauthrs Verifiers.Jobseeker ➔ mcp-go StreamableHTTP)
                             └─ domain/jobseeker.Service ──gRPC(x-ctxinfo-bin)──▶ job-go / jobseeker-go / config-go
           ──Bearer JWT──▶ /recruiter/mcp (oauthrs Verifiers.Recruiter ➔ mcp-go StreamableHTTP)
                             └─ domain/recruiter.Service ──gRPC(x-ctxinfo-bin)──▶ recruiter-go / job-go / talent-go / job-app-go / config-go
```

- `domain/mcpkit`：两端共用的错误转换、分页、站点表、config-go 字典解析（`Resolver`）与薪资洞察。

- 本服务是 BFF：不连数据库 / Redis，下游**只走 gRPC**（`infra/grpc.go`，`grpchelper.DefaultClientOpts` 透传 CtxInfo）。
- 依赖统一由 `dig` 管理（`di/di.go`），目录结构参考 `pay`：`cmd/`（cobra）、`conf/`、`di/`、`infra/`、`domain/`、`app/`、`tests/`。

### 求职者工具

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

### 招聘者工具

| 工具 | 下游 gRPC |
| :--- | :--- |
| `get_my_recruiter_profile` | `recruiter.v3.RecruiterProfileService/GetMyProfile`（额度、公司、可招聘状态） |
| `list_my_jobs` / `get_my_job` | `job.v3.JobManagementService/ManageJobList`（`recruiter_id = uid`）/ `ManageJobDetail` |
| `search_talents` | `talent.v3.TalentSearchService/SearchTalent`；地点 / 经验 / 学历 / 到岗 / 语言名称经 `GetCountryConfig` 解析，page_token 携带 `query_id` |
| `list_recommended_talents` | `talent.v3.TalentSearchService/GetRecoTalent` |
| `get_talent` | `talent.v3.TalentService/GetTalentDetail`（`talent_id = users.id`；`source = device = mcp`，与 Web 一致记录浏览并受频控） |
| `list_job_candidates` | `talent.v3.TalentService/GetChattedTalents` \| `GetSavedMyJobTalents` \| `GetViewedMyJobTalents` |
| `list_saved_talents` / `save_talent` / `unsave_talent` | `talent.v3.TalentService/GetSavedTalents` / `SaveTalent`（会通知求职者）/ `DeleteSavedTalent`（NotFound 视为成功） |
| `list_interviews` | `job_app.v3.RecruiterInterviewService/ListInterviews` |
| `get_recruitment_stats` | `recruiter.v3.RecruiterStatService/GetRecruitmentData`（TODAY：今天 + 昨天；其余 `time_category_id` 2/3/4） |
| `get_salary_insight` | 同求职者（`mcpkit.SalaryInsight` ➔ `SearchSalary`），默认站点为所属公司国家 |

招聘者端红线：不提供扣额度或主动触达求职者的操作；人才数据统一脱敏（`domain/recruiter/convert.go`）——姓名 `First L.`、不映射联系方式 / 年龄 / 性别 / 头像 / 文件链接、自由文本中的邮箱与电话替换为占位符。

## 鉴权（OAuth 资源服务器）

- Token 由 auth-go 签发（dev issuer `https://dev.auth.bossjob.com`），RS256，公钥取自 `{issuer}/.well-known/jwks.json`（按 kid 缓存，未知 kid 按最短间隔刷新）。
- 校验（每个端点独立的 Verifier）：签名 + kid、`iss`、`aud = {jobseeker,recruiter}_resource`、`exp`（必填）/`iat`/`nbf`、`scope` 含 `mcp:access`、`role` 与端点一致、`uid` 为正整数；求职者 Token 访问招聘者端点（或反之）一律 401。
- 失败：401 `WWW-Authenticate: Bearer resource_metadata="<origin>/.well-known/oauth-protected-resource/{jobseeker,recruiter}/mcp"`；缺 scope 为 403 `insufficient_scope`。
- RFC 9728 元数据：`GET /.well-known/oauth-protected-resource/{jobseeker,recruiter}/mcp`（`authorization_servers = [issuer]`）。
- 下游 CtxInfo：`UserID = uid`、`RoleID` 由 `role` 解析、`LanguageID = 1`（英语）、`SiteID = 1`、`DeviceType = mcp`。
  - 求职者：`CountryID = country_id`（auth-go 登录页选择的站点；缺失回退 167），**不传 SID**；工具参数 `country_code` 仅切换本次调用的站点。
  - 招聘者：按 uid 调 `recruiter.v3.RecruiterInternalService/GetRecruiterContextInternal`（进程内缓存 5 分钟），`SID = recruiter_sid`、`CountryID = 所属公司 country_id`（未加入公司回退 Token `country_id`）；解析失败降级为 Token 声明且不带 SID，不阻断工具。

## 配置

`-c` 指定文件，否则 `{ENV}_config.yaml`，再否则 `config.yaml`。dev 见 `development_config.yaml`，生产示例见 `config.example.yaml`。

| Key | 说明 |
| :--- | :--- |
| `map_config.oauth.issuer` | auth-go issuer（无尾斜杠 origin） |
| `map_config.oauth.{jobseeker,recruiter}_resource` | 与 auth-go `clients[].bindings.resource` 完全一致，其 path 必须等于 `mcp.{jobseeker,recruiter}_path` |
| `map_config.grpc.{job,jobseeker,config,recruiter,talent,job_app}` | 下游 gRPC 地址（:9000） |
| `map_config.mcp.allowed_hosts` | DNS rebinding 防护的 Host 白名单 |
| `map_config.mcp.web_base_url` | 相对 `job_url` / `company_url` 的站点根地址，可带路径前缀（dev：`https://demo.bossjob.com/en-us/`） |

## 运行与测试

```sh
GOWORK=off go run ./cmd serve -c development_config.yaml   # 监听 :8000，/health 探针
GOWORK=off ./citest.sh                                       # TestCI_*，下游 gRPC 通过 dig Decorate 替换为 fake
```

## 已知限制（v1）

- `list_my_applications`：下游 `JobApplicationListItem` 无公司名、投递时间与职位链接，对应字段为空。
- `search_jobs.experience_levels` 无法识别的名称会被忽略；`get_salary_insight` 无法识别职位 / 地点时返回 NotFound。
- 依赖 config-go gRPC（`GetCountryConfig` / `SearchJobTitleLists`），config-go 的 k8s Service 需暴露 9000。
- 招聘者：`list_job_candidates` 中 SAVED / VIEWED 来源无互动时间，CHATTED 来源无职位名称；`get_recruitment_stats` 的日期按 mcp-server 进程时区取“今天”。
