# mcp-server 项目记忆

- **Token 声明**：auth-go（`codex/oauth-authorization-server`）JWT 含 `sub`（不透明）、`uid`（内部用户 ID）、`country_id`（`users.registered_country_id`，缺失 167）、`role`、`scope`、`aud`；RoleID / LanguageID / SID 刻意不进 Token，由本服务组装（RoleID 由 role、LanguageID=1、SID 不传）。
- **config-go 字典**：过滤版 `GetConfigList` 只支持少数 key 且不按国家过滤，`location_lists` / `xp_lvls` 必须用 `GetCountryConfig(country_key, language_code)`；`xp_lvls` 条目的 SEO 字段 key 是 `seo-value`（连字符）；Struct 数字均为 float64。
- **job-go 搜索**：`SearchJobsRequest.job_locations` 接受城市名（job-go 查 `location_lists.value` 解析），但 `xp_lvls`（文本）未被 job-go 使用，只能传 `xp_lvl_ids`；薪资过滤 from/to 必须成对。
- **站点**：下游按 CtxInfo.CountryID 区分国家站点（默认 167=PH）；country_code ↔ id 映射与 api-go `CountryIdKeyMap` 一致。
- **mcp-go**：使用 v1.x `StreamableHTTPServer` 无状态模式；工具注册经 `redpanda-data/protoc-gen-go-mcp/pkg/runtime/mark3labs` 适配器；模块下载若卡在 goproxy.io，改用 `GOPROXY=https://proxy.golang.org,direct`。
