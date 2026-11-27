# mcp-server 项目记忆

- **Token 声明**：auth-go（`codex/oauth-authorization-server`）JWT 含 `sub`（不透明）、`uid`（内部用户 ID）、`country_id`（auth-go 登录页选择的站点，存 `oauth_grant.country_id`，缺失 167）、`role`、`scope`、`aud`；RoleID / LanguageID / SID 刻意不进 Token，由本服务组装（RoleID 由 role、LanguageID=1、SID 不传）。
- **config-go 字典**：过滤版 `GetConfigList` 只支持少数 key 且不按国家过滤，`location_lists` / `xp_lvls` 必须用 `GetCountryConfig(country_key, language_code)`；`xp_lvls` 条目的 SEO 字段 key 是 `seo-value`（连字符）；Struct 数字均为 float64。
- **job-go 搜索**：`SearchJobsRequest.job_locations` 接受城市名（job-go 查 `location_lists.value` 解析），但 `xp_lvls`（文本）未被 job-go 使用，只能传 `xp_lvl_ids`；薪资过滤 from/to 必须成对。
- **站点**：下游按 CtxInfo.CountryID 区分国家站点（默认 167=PH）；站点表与 jobseeker-web `helpers/country.base.js` countryConfig（19 个，含 241 Others=`com`，不含 MO）及 auth-go `oauth.SiteCountries` 保持一致，改动需三处同步。
- **mcp-go**：使用 v1.x `StreamableHTTPServer` 无状态模式；工具注册经 `redpanda-data/protoc-gen-go-mcp/pkg/runtime/mark3labs` 适配器；模块下载若卡在 goproxy.io，改用 `GOPROXY=https://proxy.golang.org,direct`。
- **招聘者身份**：Token 只有 uid / country_id；`recruiter_sid` 与公司国家站点由 recruiter-go `GetRecruiterContextInternal(user_id)` 提供（进程内缓存 5 分钟，失败降级为 Token 声明）。站点以所属公司为准（产品决策 2026-11-28）。
- **talent-go 人才 ID**：`GetTalentDetailV2.talent_id` 实际按 `users.jobseeker_sid` 查询，`GetTalentDetail` / `SaveTalent` / `DeleteSavedTalent` 用 `users.id`；MCP 统一用 `users.id`（`get_talent` ➔ `GetTalentDetail`）。人才详情只要传 source / device 就写 viewed_profiles 并通知求职者，且受每招聘者每小时 500 次频控（40008）；`SaveTalent` 会给求职者发 App 通知；`DeleteSavedTalent` 未收藏返回 NotFound。
- **job-go `SalaryBenchmarks`** 不分国家、无币种，不适合回答“该开多少薪”；招聘者薪资洞察复用 `JobSearchService/SearchSalary`（无角色限制）。
- **排序枚举**：talent `TalentSearchSort` DEFAULT=0 / ACTIVE=1 / RELEVANT=2；`TalentRecoSort` 0/1 = 最新活跃，2 = 综合推荐。
- **招聘数据**：`GetRecruitmentData.time_category_id = 0` 返回 today + yesterday，1/2/3/4 返回日 / 周 / 月 / 年聚合（`aggregate`）；`date` 格式 `2006-01-02`。

