package tests

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobappv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job_app/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	recruiterv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/recruiter/v3"
	talentv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/talent/v3"
	"github.com/bytedance/mockey"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mark3labs/mcp-go/client"
	"github.com/smartystreets/goconvey/convey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"bossjob-mcp-server/domain/oauthrs"
	"bossjob-mcp-server/pub/digutil"
)

const (
	testRecruiterSID   = 7700001
	testCompanyCountry = 193 // SG：与 Token country_id(167) 不同，用于断言站点以公司为准
)

// recruiterEnv 已登录招聘者的测试环境：身份上下文返回 SID 与公司国家（SG）。
func recruiterEnv(t *testing.T) (*env, *client.Client) {
	e := newEnv(t)
	e.rec.set("GetRecruiterContextInternal", &recruiterv3.GetRecruiterContextInternalResponse{
		UserId: testUserID, RecruiterSid: testRecruiterSID, StatusKey: "active", CompanyId: 9,
		WorkXpStatusKey: "active", CompanyCountryId: testCompanyCountry, IsCompanyVerified: true, IsCompanyActive: true,
	}, nil)
	return e, e.clientAt(t, "/recruiter/mcp", signRecruiterToken(t, nil))
}

// assertRecruiterCtx 下游收到的 CtxInfo：招聘者角色、recruiter_sid、公司国家站点。
func assertRecruiterCtx(c call) {
	convey.So(c.cinfo.UserID, convey.ShouldEqual, testUserID)
	convey.So(c.cinfo.RoleID, convey.ShouldEqual, 2)
	convey.So(c.cinfo.SID, convey.ShouldEqual, testRecruiterSID)
	convey.So(c.cinfo.CountryID, convey.ShouldEqual, testCompanyCountry)
	convey.So(c.cinfo.LanguageID, convey.ShouldEqual, 1)
	convey.So(c.cinfo.DeviceType, convey.ShouldEqual, "mcp")
}

func talentDetailFixture(id int64) *talentv3.TalentDetail {
	return &talentv3.TalentDetail{
		Id: id, JobseekerSid: 990000 + id, FirstName: "Wei Ming", LastName: "lee", FullName: "Wei Ming Lee",
		Email: "wei@example.com", PhoneNum: "+6591234567", Birthdate: "1990-01", Age: 34, Gender: "male",
		Avatar: "https://cdn/avatar.png", Description: "Reach me at wei@example.com or +65 9123 4567. Go expert since 2012-2016, certified 2020.01.15.",
		XpLvl: "3 - 5 Yrs", DegreeValue: "Bachelor", EduSchoolName: "NUS", LocationValue: "Singapore",
		CurrentCompanyName: "Acme", CurrentJobTitle: "Backend Engineer", IsCurrentlyWorkHere: true,
		LocalSalaryRangeValue: "S$6K - S$8K / month", NoticePeriodValue: "Within 1 month",
		JobseekingStatusId: talentv3.JobseekingStatus_JOBSEEKING_STATUS_ACTIVELY_LOOKING,
		Skills:             []string{"Go", "gRPC"}, CardTags: []*talentv3.CardTagItem{{T: "Top school"}, {T: ""}},
		IsOnline: true, ActiveStatus: &talentv3.TalentActiveStatus{Value: "Active today"}, LastActiveAt: timestamppb.Now(),
		IsSavedTalent: true, IsUnlockedTalent: false,
		UserNationality:    &talentv3.TalentUserNationality{CountryId: 193, CountryKey: "singapore"},
		WorkingYearsMonths: &talentv3.TalentWorkingYearsMonths{Years: 5, Months: 3},
		WorkExperiences: []*talentv3.TalentWorkXp{{Company: "Acme", JobTitle: "Backend Engineer", WorkingPeriodFrom: "2020-01",
			WorkingPeriodTo: "2026-01", IsCurrentlyWorkHere: true, Description: "Call 9123-4567-88 for refs"}},
		Educations: []*talentv3.TalentEducation{{School: "NUS", DegreeValue: "Bachelor", FieldOfStudy: "CS",
			StudyPeriodFrom: "2012-08", StudyPeriodTo: "2016-05"}},
		Languages:             []*talentv3.TalentLanguage{{LanguageValue: "English", ProficiencyValue: "Fluent"}, {LanguageValue: "Mandarin"}},
		LicenseCertifications: []*talentv3.TalentLicenseCertification{{Name: "CKA", CredentialUrl: "https://cert/secret"}},
		JobPreferences: []*talentv3.TalentPreference{{JobTitle: "Staff Engineer", Location: "Singapore",
			LocalSalaryRangeValue: "S$9K - S$11K / month", WorkArrangementId: talentv3.WorkArrangement_WORK_ARRANGEMENT_HYBRID}},
		Resume:       &talentv3.TalentResume{Id: 5, Url: "https://cdn/resume.pdf"},
		VideoResumes: []*talentv3.TalentVideoResume{{Id: 1, Url: "https://cdn/v.mp4"}},
		Websites:     []*talentv3.TalentWebsite{{}},
	}
}

func talentCardFixture(id int64) *talentv3.TalentCardItem {
	return &talentv3.TalentCardItem{
		Id: id, FirstName: "Ana", LastName: "santos", FullName: "Ana Santos", Avatar: "https://cdn/a.png", Age: 28, Gender: "female",
		YearsOfExperience: 4, Skills: []string{"Excel"}, LocalSalaryRangeValue: "₱40K - ₱60K / month",
		WorkExperience:   &talentv3.TalentWorkXp{Company: "BDO", JobTitle: "Accountant", IsCurrentlyWorkHere: true},
		Education:        &talentv3.TalentEducation{School: "UP", DegreeValue: "Bachelor"},
		LatestPreference: &talentv3.TalentLatestPreference{Location: "Makati"},
		IsChattedTalent:  true, NoticePeriodValue: "Immediately",
	}
}

// assertNoPII 输出不得包含联系方式、生日、头像、简历等敏感字段或原始全名。
func assertNoPII(text string) {
	for _, s := range []string{"wei@example.com", "6591234567", "9123 4567", "9123-4567", "1990-01", "avatar.png", "a.png",
		"resume.pdf", "v.mp4", "cert/secret", "Wei Ming Lee", "Ana Santos", "male"} {
		convey.So(text, convey.ShouldNotContainSubstring, s)
	}
}

func TestCI_RecruiterMcp_Auth(t *testing.T) {
	mockey.PatchConvey("Feature: 招聘者 MCP 端点只接受招聘者 Token，并与求职者端点相互隔离", t, func() {
		e := newEnv(t)
		srv := e.server(t)
		post := func(path, token string) *http.Response {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			convey.So(err, convey.ShouldBeNil)
			return resp
		}

		mockey.PatchConvey("Scenario: 求职者 Token 访问招聘者端点 / 招聘者 Token 访问求职者端点均 401", func() {
			resp := post("/recruiter/mcp", signToken(t, nil))
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusUnauthorized)
			convey.So(resp.Header.Get("WWW-Authenticate"), convey.ShouldContainSubstring,
				`resource_metadata="https://dev.mcp.bossjob.com/.well-known/oauth-protected-resource/recruiter/mcp"`)
			convey.So(post("/jobseeker/mcp", signRecruiterToken(t, nil)).StatusCode, convey.ShouldEqual, http.StatusUnauthorized)
			// 招聘者角色但 aud 为求职者 resource
			convey.So(post("/recruiter/mcp", signRecruiterToken(t, jwt.MapClaims{"aud": testResource})).StatusCode, convey.ShouldEqual, http.StatusUnauthorized)
		})
		mockey.PatchConvey("Scenario: 招聘者受保护资源元数据", func() {
			resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/recruiter/mcp")
			convey.So(err, convey.ShouldBeNil)
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusOK)
		})
		mockey.PatchConvey("Scenario: Verifiers.Recruiter 解析招聘者 Token", func() {
			p, err := digutil.MustGet[oauthrs.Verifiers](e.c).Recruiter.Verify(context.Background(), signRecruiterToken(t, nil))
			convey.So(err, convey.ShouldBeNil)
			convey.So(p.Role, convey.ShouldEqual, oauthrs.RoleRecruiter)
			convey.So(p.RoleID(), convey.ShouldEqual, 2)
		})
		mockey.PatchConvey("Scenario: 合法招聘者 Token 列出 13 个工具", func() {
			cli := e.clientAt(t, "/recruiter/mcp", signRecruiterToken(t, nil))
			tools, err := cli.ListTools(context.Background(), mcpListToolsRequest())
			convey.So(err, convey.ShouldBeNil)
			names := make([]string, 0, len(tools.Tools))
			for _, tool := range tools.Tools {
				names = append(names, tool.Name)
			}
			convey.So(names, convey.ShouldHaveLength, 13)
			for _, n := range []string{"get_my_recruiter_profile", "list_my_jobs", "get_my_job", "search_talents",
				"list_recommended_talents", "get_talent", "list_job_candidates", "list_saved_talents", "save_talent",
				"unsave_talent", "list_interviews", "get_recruitment_stats", "get_salary_insight"} {
				convey.So(names, convey.ShouldContain, n)
			}
		})
	})
}

func TestCI_RecruiterMcp_Identity(t *testing.T) {
	mockey.PatchConvey("Feature: 招聘者身份上下文（SID + 公司国家站点）按 uid 缓存并降级", t, func() {
		mockey.PatchConvey("Scenario: 多次调用只解析一次身份，下游 CtxInfo 带 SID 与公司国家", func() {
			e, cli := recruiterEnv(t)
			e.rec.set("RecruiterGetMyProfile", &recruiterv3.GetMyProfileResponse{Profile: &recruiterv3.RecruiterMyProfile{}}, nil)
			for i := 0; i < 3; i++ {
				res, _ := callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, nil)
				convey.So(res.IsError, convey.ShouldBeFalse)
			}
			convey.So(e.rec.count("GetRecruiterContextInternal"), convey.ShouldEqual, 1)
			ic, _ := e.rec.last("GetRecruiterContextInternal")
			convey.So(ic.req.(*recruiterv3.GetRecruiterContextInternalRequest).UserId, convey.ShouldEqual, testUserID)
			c, _ := e.rec.last("RecruiterGetMyProfile")
			assertRecruiterCtx(c)
		})
		mockey.PatchConvey("Scenario: 身份解析失败时回退 Token country_id 且不带 SID，不阻断工具", func() {
			e := newEnv(t)
			e.rec.set("GetRecruiterContextInternal", nil, status.Error(codes.Unavailable, "down"))
			e.rec.set("RecruiterGetMyProfile", &recruiterv3.GetMyProfileResponse{Profile: &recruiterv3.RecruiterMyProfile{}}, nil)
			cli := e.clientAt(t, "/recruiter/mcp", signRecruiterToken(t, nil))
			res, _ := callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, nil)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("RecruiterGetMyProfile")
			convey.So(c.cinfo.CountryID, convey.ShouldEqual, 167)
			convey.So(c.cinfo.SID, convey.ShouldEqual, 0)
		})
		mockey.PatchConvey("Scenario: 未加入公司（company_country_id=0）时使用 Token country_id", func() {
			e := newEnv(t)
			e.rec.set("GetRecruiterContextInternal", &recruiterv3.GetRecruiterContextInternalResponse{UserId: testUserID, RecruiterSid: 5}, nil)
			e.rec.set("RecruiterGetMyProfile", &recruiterv3.GetMyProfileResponse{Profile: &recruiterv3.RecruiterMyProfile{}}, nil)
			cli := e.clientAt(t, "/recruiter/mcp", signRecruiterToken(t, jwt.MapClaims{"country_id": 105}))
			callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, nil)
			c, _ := e.rec.last("RecruiterGetMyProfile")
			convey.So(c.cinfo.CountryID, convey.ShouldEqual, 105)
			convey.So(c.cinfo.SID, convey.ShouldEqual, 5)
		})
	})
}

func TestCI_RecruiterMcp_GetMyRecruiterProfile(t *testing.T) {
	mockey.PatchConvey("Feature: get_my_recruiter_profile 返回账号摘要、额度与可招聘状态（不含联系方式）", t, func() {
		e, cli := recruiterEnv(t)
		profile := func(xpStatus string, verified bool) *recruiterv3.GetMyProfileResponse {
			return &recruiterv3.GetMyProfileResponse{Profile: &recruiterv3.RecruiterMyProfile{
				FullName: "Rita Cruz", Email: "rita@acme.test", PhoneNum: "+639171234567",
				WorkExperience:   &recruiterv3.RecruiterWorkExperience{JobTitle: "HR Manager", CompanyId: 9, StatusKey: xpStatus},
				Company:          &recruiterv3.Company{Id: 9, Name: "Acme", IsVerify: verified, CompanyUrl: "/company/acme-9", Email: "hr@acme.test"},
				AvailableCredits: &recruiterv3.AvailableCredits{JobPost: 3, UnlockTalent: 10, FeatureJob: 1, UrgentJob: 2},
				Subscription:     &recruiterv3.Subscription{Name: "Pro"}, ActiveJobNum: 4, ResponseRate: 88, ResponseTime: "Within 1 day",
			}}
		}

		mockey.PatchConvey("Scenario: 已加入并认证公司", func() {
			e.rec.set("RecruiterGetMyProfile", profile("active", true), nil)
			var out openv1.GetMyRecruiterProfileResponse
			res, text := callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			p := out.Profile
			convey.So(p.FullName, convey.ShouldEqual, "Rita Cruz")
			convey.So(p.JobTitle, convey.ShouldEqual, "HR Manager")
			convey.So(p.Company.Name, convey.ShouldEqual, "Acme")
			convey.So(p.Company.CompanyUrl, convey.ShouldEqual, "https://demo.bossjob.com/en-us/company/acme-9")
			convey.So(p.IsReadyToHire, convey.ShouldBeTrue)
			convey.So(p.Credits.UnlockTalent, convey.ShouldEqual, 10)
			convey.So(p.Credits.FeaturedJob, convey.ShouldEqual, 1)
			convey.So(p.SubscriptionPlan, convey.ShouldEqual, "Pro")
			convey.So(p.ActiveJobCount, convey.ShouldEqual, 4)
			convey.So(text, convey.ShouldNotContainSubstring, "rita@acme.test")
			convey.So(text, convey.ShouldNotContainSubstring, "639171234567")
			convey.So(text, convey.ShouldNotContainSubstring, "hr@acme.test")
		})
		mockey.PatchConvey("Scenario: 加入申请待审核 / 公司未认证时给出原因", func() {
			e.rec.set("RecruiterGetMyProfile", profile("pending", true), nil)
			var out openv1.GetMyRecruiterProfileResponse
			callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, &out)
			convey.So(out.Profile.IsReadyToHire, convey.ShouldBeFalse)
			convey.So(out.Profile.NotReadyReason, convey.ShouldContainSubstring, "pending")

			e.rec.set("RecruiterGetMyProfile", profile("active", false), nil)
			callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, &out)
			convey.So(out.Profile.IsReadyToHire, convey.ShouldBeFalse)
			convey.So(out.Profile.NotReadyReason, convey.ShouldContainSubstring, "verification")
		})
		mockey.PatchConvey("Scenario: 下游内部错误统一为 temporarily unavailable", func() {
			e.rec.set("RecruiterGetMyProfile", nil, status.Error(codes.Internal, "db password leaked"))
			res, text := callTool(t, cli, "get_my_recruiter_profile", map[string]any{}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "temporarily unavailable")
			convey.So(text, convey.ShouldNotContainSubstring, "password")
		})
	})
}

func TestCI_RecruiterMcp_ListMyJobs(t *testing.T) {
	mockey.PatchConvey("Feature: list_my_jobs 只列当前招聘者的职位", t, func() {
		e, cli := recruiterEnv(t)
		e.rec.set("ManageJobList", &jobv3.ManageJobListResponse{
			TotalNum: 25, TotalPages: 3, TotalActiveJobs: 20, TotalPendingJobs: 2, TotalClosedJobs: 3, TotalDraftJobs: 1,
			Jobs: []*jobv3.ManageJobListItem{{
				Id: 31, JobTitle: "Accountant", StatusKey: "change_required", JobLocation: "Makati", CountryId: 167,
				SalaryType: &jobv3.ManageJobListSalaryType{Id: 1}, SalaryRangeFrom: 30000, SalaryRangeTo: 45000.5,
				LocalSalaryRangeValue: "₱30K - ₱45K / month", WorkArrangement: &jobv3.WorkArrangementDTO{Id: 3},
				XpLvl: "1 - 3 Yrs", NoOfViews: 120, JobApplicationCount: &jobv3.ManageJobApplicationCount{Total: 7},
				ChangeRequiredReason: "Add salary details", JobUrl: "/job/accountant-31",
			}},
		}, nil)

		mockey.PatchConvey("Scenario: 默认状态过滤 + recruiter_id 固定为当前用户", func() {
			var out openv1.ListMyJobsResponse
			res, _ := callTool(t, cli, "list_my_jobs", map[string]any{"query": "account", "page_size": 5}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("ManageJobList")
			req := c.req.(*jobv3.ManageJobListRequest)
			convey.So(req.GetRecruiterId(), convey.ShouldEqual, testUserID)
			convey.So(req.Status, convey.ShouldEqual, "active,pending,change_required,rejected,closed")
			convey.So(req.Query, convey.ShouldEqual, "account")
			convey.So(req.Page, convey.ShouldEqual, 1)
			convey.So(req.Size, convey.ShouldEqual, 5)
			assertRecruiterCtx(c)

			j := out.Jobs[0]
			convey.So(j.Status, convey.ShouldEqual, openv1.ManagedJobStatus_MANAGED_JOB_STATUS_CHANGE_REQUIRED)
			convey.So(j.ReviewFeedback, convey.ShouldEqual, "Add salary details")
			convey.So(j.Salary.CurrencyCode, convey.ShouldEqual, "PHP")
			convey.So(j.Salary.MinAmount, convey.ShouldEqual, "30000")
			convey.So(j.Salary.MaxAmount, convey.ShouldEqual, "45000.5")
			convey.So(j.WorkArrangement, convey.ShouldEqual, openv1.WorkArrangement_WORK_ARRANGEMENT_REMOTE)
			convey.So(j.ApplicantCount, convey.ShouldEqual, 7)
			convey.So(j.JobUrl, convey.ShouldEqual, "https://demo.bossjob.com/en-us/job/accountant-31")
			convey.So(out.ActiveCount, convey.ShouldEqual, 20)
			convey.So(out.TotalSize, convey.ShouldEqual, 25)
			convey.So(out.NextPageToken, convey.ShouldNotBeEmpty)
		})
		mockey.PatchConvey("Scenario: 指定状态与翻页", func() {
			var first openv1.ListMyJobsResponse
			callTool(t, cli, "list_my_jobs", map[string]any{}, &first)
			callTool(t, cli, "list_my_jobs", map[string]any{
				"statuses":   []any{"MANAGED_JOB_STATUS_DRAFT", "MANAGED_JOB_STATUS_ACTIVE", "MANAGED_JOB_STATUS_DRAFT"},
				"page_token": first.NextPageToken,
			}, nil)
			c, _ := e.rec.last("ManageJobList")
			req := c.req.(*jobv3.ManageJobListRequest)
			convey.So(req.Status, convey.ShouldEqual, "draft,active")
			convey.So(req.Page, convey.ShouldEqual, 2)
		})
	})
}

func TestCI_RecruiterMcp_GetMyJob(t *testing.T) {
	mockey.PatchConvey("Feature: get_my_job 返回职位详情", t, func() {
		e, cli := recruiterEnv(t)
		mockey.PatchConvey("Scenario: 映射 JD、技能、福利与薪资", func() {
			e.rec.set("ManageJobDetail", &jobv3.ManageJobDetailResponse{
				Id: 31, JobTitle: "Accountant", StatusKey: "active", JobTypeId: 1, FunctionJobTitle: "Accounting",
				CountryId: 193, SalaryTypeId: 2, DailySalaryRangeFrom: 150, DailySalaryRangeTo: 200, LocalDailySalaryRangeValue: "S$150 - S$200 / day",
				JobSkills: []string{"Excel"}, JobBenefits: []*jobv3.NamedItemDTO{{Name: "Medical"}},
				Languages: []*jobv3.LanguageDTO{{Name: "English"}}, Address: "1 Raffles Place", FloorUnit: "#10-01",
				JobDescription: "Close books", JobRequirements: "CPA", NoOfViews: 9,
			}, nil)
			var out openv1.GetMyJobResponse
			res, _ := callTool(t, cli, "get_my_job", map[string]any{"job_id": 31}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("ManageJobDetail")
			convey.So(c.req.(*jobv3.ManageJobDetailRequest).JobId, convey.ShouldEqual, 31)
			j := out.Job
			convey.So(j.Summary.Status, convey.ShouldEqual, openv1.ManagedJobStatus_MANAGED_JOB_STATUS_ACTIVE)
			convey.So(j.Summary.Salary.Period, convey.ShouldEqual, openv1.SalaryPeriod_SALARY_PERIOD_DAILY)
			convey.So(j.Summary.Salary.MinAmount, convey.ShouldEqual, "150")
			convey.So(j.Summary.Salary.CurrencyCode, convey.ShouldEqual, "SGD")
			convey.So(j.JobType, convey.ShouldEqual, openv1.JobType_JOB_TYPE_FULL_TIME)
			convey.So(j.Benefits, convey.ShouldResemble, []string{"Medical"})
			convey.So(j.FullAddress, convey.ShouldEqual, "#10-01, 1 Raffles Place")
			convey.So(j.Description, convey.ShouldEqual, "Close books")
		})
		mockey.PatchConvey("Scenario: 非本人职位由下游拒绝并透传；job_id 非法时本地拒绝", func() {
			e.rec.set("ManageJobDetail", nil, status.Error(codes.PermissionDenied, "job not owned"))
			res, text := callTool(t, cli, "get_my_job", map[string]any{"job_id": 32}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "job not owned")
			before := e.rec.count("ManageJobDetail")
			res, _ = callTool(t, cli, "get_my_job", map[string]any{"job_id": 0}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(e.rec.count("ManageJobDetail"), convey.ShouldEqual, before)
		})
	})
}

func TestCI_RecruiterMcp_SearchTalents(t *testing.T) {
	mockey.PatchConvey("Feature: search_talents 将名称筛选解析为 ID 并脱敏返回", t, func() {
		e, cli := recruiterEnv(t)
		e.rec.set("SearchTalent", &talentv3.SearchTalentResponse{
			Talents: []*talentv3.TalentDetail{talentDetailFixture(501)}, TotalNum: 40, TotalPages: 4, QueryId: "qid-1",
		}, nil)

		mockey.PatchConvey("Scenario: 关键词 + 地点 + 经验 + 学历 + 到岗 + 语言 + 职位", func() {
			var out openv1.SearchTalentsResponse
			res, text := callTool(t, cli, "search_talents", map[string]any{
				"query": "golang", "locations": []any{"Makati", "Central Visayas", "Atlantis"}, "experience_levels": []any{"1 - 3 yrs"},
				"degrees": []any{"bachelor"}, "notice_periods": []any{"Immediately"}, "languages": []any{"EN", "Klingon"},
				"currently_employed_only": true, "online_only": true, "job_id": 31, "sort_order": "TALENT_SORT_ORDER_RECENT_ACTIVE", "page_size": 5,
			}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("SearchTalent")
			req := c.req.(*talentv3.SearchTalentRequest)
			convey.So(req.Query, convey.ShouldEqual, "golang")
			convey.So(req.LocationIds, convey.ShouldEqual, "201")
			convey.So(req.RegionIds, convey.ShouldEqual, "10")
			convey.So(req.XpLvlIds, convey.ShouldEqual, "2")
			convey.So(req.EduDegreeIds, convey.ShouldEqual, "3")
			convey.So(req.NoticePeriodIds, convey.ShouldEqual, "1")
			convey.So(req.LanguageIds, convey.ShouldEqual, "1")
			convey.So(req.IsCurrentlyWorkHere, convey.ShouldBeTrue)
			convey.So(req.IsOnline, convey.ShouldBeTrue)
			convey.So(req.JobId, convey.ShouldEqual, 31)
			convey.So(req.Sort, convey.ShouldEqual, talentv3.TalentSearchSort_TALENT_SEARCH_SORT_ACTIVE)
			convey.So(req.CountryId, convey.ShouldEqual, testCompanyCountry)
			convey.So(req.Source, convey.ShouldEqual, "mcp")
			convey.So(req.MinAge, convey.ShouldEqual, 0)
			assertRecruiterCtx(c)
			cc, _ := e.rec.last("GetCountryConfig")
			convey.So(cc.req.(*configv3.GetCountryConfigRequest).CountryKey, convey.ShouldEqual, "sg")

			tl := out.Talents[0]
			convey.So(tl.TalentId, convey.ShouldEqual, 501)
			convey.So(tl.DisplayName, convey.ShouldEqual, "Wei L.")
			convey.So(tl.CurrentJobTitle, convey.ShouldEqual, "Backend Engineer")
			convey.So(tl.ExpectedSalary, convey.ShouldEqual, "S$6K - S$8K / month")
			convey.So(tl.JobseekingStatus, convey.ShouldEqual, "Actively looking")
			convey.So(tl.Nationality, convey.ShouldEqual, "SG")
			convey.So(tl.Tags, convey.ShouldResemble, []string{"Top school"})
			convey.So(tl.IsSaved, convey.ShouldBeTrue)
			assertNoPII(text)
			convey.So(out.TotalSize, convey.ShouldEqual, 40)

			// 翻页携带 query_id
			callTool(t, cli, "search_talents", map[string]any{"query": "golang", "page_token": out.NextPageToken}, nil)
			c, _ = e.rec.last("SearchTalent")
			convey.So(c.req.(*talentv3.SearchTalentRequest).Page, convey.ShouldEqual, 2)
			convey.So(c.req.(*talentv3.SearchTalentRequest).QueryId, convey.ShouldEqual, "qid-1")
		})
		mockey.PatchConvey("Scenario: 默认排序为综合推荐；非法 page_token 拒绝", func() {
			callTool(t, cli, "search_talents", map[string]any{"query": "x"}, nil)
			c, _ := e.rec.last("SearchTalent")
			convey.So(c.req.(*talentv3.SearchTalentRequest).Sort, convey.ShouldEqual, talentv3.TalentSearchSort_TALENT_SEARCH_SORT_DEFAULT)
			res, text := callTool(t, cli, "search_talents", map[string]any{"page_token": "bogus"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "page_token")
		})
	})
}

func TestCI_RecruiterMcp_ListRecommendedTalents(t *testing.T) {
	mockey.PatchConvey("Feature: list_recommended_talents 调用职位推荐", t, func() {
		e, cli := recruiterEnv(t)
		e.rec.set("GetRecoTalent", &talentv3.GetRecoTalentResponse{
			Talents: []*talentv3.TalentDetail{talentDetailFixture(502)}, TotalNum: 1, TotalPages: 1, QueryId: "q2",
		}, nil)
		mockey.PatchConvey("Scenario: 默认综合推荐 + 筛选", func() {
			var out openv1.ListRecommendedTalentsResponse
			res, text := callTool(t, cli, "list_recommended_talents", map[string]any{"job_id": 31, "degrees": []any{"Master"}}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetRecoTalent")
			req := c.req.(*talentv3.GetRecoTalentRequest)
			convey.So(req.JobId, convey.ShouldEqual, 31)
			convey.So(req.Sort, convey.ShouldEqual, talentv3.TalentRecoSort_TALENT_RECO_SORT_RECALL)
			convey.So(req.DegreeIds, convey.ShouldEqual, "4")
			assertRecruiterCtx(c)
			convey.So(out.Talents[0].DisplayName, convey.ShouldEqual, "Wei L.")
			convey.So(out.NextPageToken, convey.ShouldBeEmpty)
			assertNoPII(text)
		})
		mockey.PatchConvey("Scenario: RECENT_ACTIVE 映射；缺少 job_id 拒绝", func() {
			callTool(t, cli, "list_recommended_talents", map[string]any{"job_id": 31, "sort_order": "TALENT_SORT_ORDER_RECENT_ACTIVE"}, nil)
			c, _ := e.rec.last("GetRecoTalent")
			convey.So(c.req.(*talentv3.GetRecoTalentRequest).Sort, convey.ShouldEqual, talentv3.TalentRecoSort_TALENT_RECO_SORT_ACTIVE)
			res, _ := callTool(t, cli, "list_recommended_talents", map[string]any{}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
		})
	})
}

func TestCI_RecruiterMcp_GetTalent(t *testing.T) {
	mockey.PatchConvey("Feature: get_talent 返回脱敏人才详情，并与 Web 一致记录浏览", t, func() {
		e, cli := recruiterEnv(t)
		mockey.PatchConvey("Scenario: 用 users.id 调 GetTalentDetail，source/device = mcp，输出去除联系方式", func() {
			e.rec.set("GetTalentDetail", talentDetailFixture(501), nil)
			var out openv1.GetTalentResponse
			res, text := callTool(t, cli, "get_talent", map[string]any{"talent_id": 501, "job_id": 31}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetTalentDetail")
			req := c.req.(*talentv3.GetTalentDetailRequest)
			convey.So(req.TalentId, convey.ShouldEqual, 501)
			convey.So(req.JobId, convey.ShouldEqual, 31)
			convey.So(req.Source, convey.ShouldEqual, "mcp")
			convey.So(req.Device, convey.ShouldEqual, "mcp")
			assertRecruiterCtx(c)

			tl := out.Talent
			convey.So(tl.Summary.DisplayName, convey.ShouldEqual, "Wei L.")
			convey.So(tl.About, convey.ShouldEqual, "Reach me at [email removed] or [phone removed]. Go expert since 2012-2016, certified 2020.01.15.")
			convey.So(tl.TotalExperience, convey.ShouldEqual, "5 years 3 months")
			convey.So(tl.WorkExperiences[0].Description, convey.ShouldEqual, "Call [phone removed] for refs")
			convey.So(tl.WorkExperiences[0].EndDate, convey.ShouldBeEmpty)
			convey.So(tl.Educations[0].FieldOfStudy, convey.ShouldEqual, "CS")
			convey.So(tl.Languages, convey.ShouldResemble, []string{"English (Fluent)", "Mandarin"})
			convey.So(tl.Certifications, convey.ShouldResemble, []string{"CKA"})
			convey.So(tl.JobPreferences[0].WorkArrangements, convey.ShouldResemble, []openv1.WorkArrangement{openv1.WorkArrangement_WORK_ARRANGEMENT_HYBRID})
			convey.So(tl.HasResume, convey.ShouldBeTrue)
			convey.So(tl.HasVideoResume, convey.ShouldBeTrue)
			assertNoPII(text)
		})
		mockey.PatchConvey("Scenario: 浏览频控等业务错误透传", func() {
			e.rec.set("GetTalentDetail", nil, status.Error(codes.ResourceExhausted, "You have made too many requests"))
			res, text := callTool(t, cli, "get_talent", map[string]any{"talent_id": 501}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "too many requests")
		})
	})
}

func TestCI_RecruiterMcp_ListJobCandidates(t *testing.T) {
	mockey.PatchConvey("Feature: list_job_candidates 按互动类型调用不同列表", t, func() {
		e, cli := recruiterEnv(t)
		created := timestamppb.New(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
		e.rec.set("GetChattedTalents", &talentv3.GetChattedTalentsResponse{TotalNum: 1, TotalPages: 1,
			ChattedTalents: []*talentv3.ChattedTalentItem{{TalentId: 601, JobId: 31, CreatedAt: created, Talent: talentCardFixture(601)}}}, nil)
		e.rec.set("GetSavedMyJobTalents", &talentv3.GetSavedMyJobTalentsResponse{TotalNum: 3, TotalPages: 2,
			SavedMyJobTalents: []*talentv3.SavedMyJobTalentItem{{TalentId: 602, JobId: 31, Job: &talentv3.InteractedJob{JobTitle: "Accountant"}}}}, nil)
		e.rec.set("GetViewedMyJobTalents", &talentv3.GetViewedMyJobTalentsResponse{
			ViewedMyJobTalents: []*talentv3.ViewedMyJobTalentItem{{TalentId: 603, JobId: 31, Talent: talentCardFixture(603)}}}, nil)

		mockey.PatchConvey("Scenario: CHATTED", func() {
			var out openv1.ListJobCandidatesResponse
			res, text := callTool(t, cli, "list_job_candidates", map[string]any{"source": "CANDIDATE_SOURCE_CHATTED", "job_id": 31}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetChattedTalents")
			assertRecruiterCtx(c)
			cand := out.Candidates[0]
			convey.So(cand.Talent.TalentId, convey.ShouldEqual, 601)
			convey.So(cand.Talent.DisplayName, convey.ShouldEqual, "Ana S.")
			convey.So(cand.Talent.ExperienceLevel, convey.ShouldEqual, "4 years")
			convey.So(cand.Talent.Location, convey.ShouldEqual, "Makati")
			convey.So(cand.InteractedAt.AsTime(), convey.ShouldEqual, created.AsTime())
			assertNoPII(text)
		})
		mockey.PatchConvey("Scenario: SAVED_MY_JOB 带 job_id，缺少人才卡片时仍返回 talent_id", func() {
			var out openv1.ListJobCandidatesResponse
			callTool(t, cli, "list_job_candidates", map[string]any{"source": "CANDIDATE_SOURCE_SAVED_MY_JOB", "job_id": 31}, &out)
			c, _ := e.rec.last("GetSavedMyJobTalents")
			convey.So(c.req.(*talentv3.GetSavedMyJobTalentsRequest).JobId, convey.ShouldEqual, 31)
			convey.So(out.Candidates[0].Talent.TalentId, convey.ShouldEqual, 602)
			convey.So(out.Candidates[0].Talent.DisplayName, convey.ShouldEqual, "Candidate")
			convey.So(out.Candidates[0].JobTitle, convey.ShouldEqual, "Accountant")
			convey.So(out.NextPageToken, convey.ShouldNotBeEmpty)
		})
		mockey.PatchConvey("Scenario: VIEWED_MY_JOB；source 必填", func() {
			var out openv1.ListJobCandidatesResponse
			callTool(t, cli, "list_job_candidates", map[string]any{"source": "CANDIDATE_SOURCE_VIEWED_MY_JOB"}, &out)
			convey.So(e.rec.count("GetViewedMyJobTalents"), convey.ShouldEqual, 1)
			convey.So(out.Candidates[0].Talent.TalentId, convey.ShouldEqual, 603)
			res, _ := callTool(t, cli, "list_job_candidates", map[string]any{}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
		})
	})
}

func TestCI_RecruiterMcp_SavedTalents(t *testing.T) {
	mockey.PatchConvey("Feature: list_saved_talents / save_talent / unsave_talent", t, func() {
		e, cli := recruiterEnv(t)
		mockey.PatchConvey("Scenario: 收藏列表", func() {
			e.rec.set("GetSavedTalents", &talentv3.GetSavedTalentsResponse{TotalNum: 1, TotalPages: 1,
				SavedTalents: []*talentv3.SavedTalentItem{{TalentId: 601, JobId: 31, CreatedAt: timestamppb.Now(), Talent: talentCardFixture(601)}}}, nil)
			var out openv1.ListSavedTalentsResponse
			res, text := callTool(t, cli, "list_saved_talents", map[string]any{"page_size": 3}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetSavedTalents")
			convey.So(c.req.(*talentv3.GetSavedTalentsRequest).Size, convey.ShouldEqual, 3)
			convey.So(out.SavedTalents[0].Talent.IsSaved, convey.ShouldBeTrue)
			convey.So(out.SavedTalents[0].JobId, convey.ShouldEqual, 31)
			assertNoPII(text)
		})
		mockey.PatchConvey("Scenario: 收藏带 job_id 与来源，CtxInfo 带 recruiter_sid", func() {
			e.rec.set("SaveTalent", &talentv3.TalentActionResponse{Message: "success"}, nil)
			var out openv1.SaveTalentResponse
			res, _ := callTool(t, cli, "save_talent", map[string]any{"talent_id": 601, "job_id": 31}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("SaveTalent")
			req := c.req.(*talentv3.SaveTalentRequest)
			convey.So(req.TalentId, convey.ShouldEqual, 601)
			convey.So(req.JobId, convey.ShouldEqual, 31)
			convey.So(req.Source, convey.ShouldEqual, "mcp")
			assertRecruiterCtx(c)
			convey.So(out.TalentId, convey.ShouldEqual, 601)
		})
		mockey.PatchConvey("Scenario: 取消收藏幂等（下游 NotFound 视为成功），其他错误透传", func() {
			e.rec.set("DeleteSavedTalent", nil, status.Error(codes.NotFound, "not saved"))
			res, _ := callTool(t, cli, "unsave_talent", map[string]any{"talent_id": 601}, nil)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("DeleteSavedTalent")
			convey.So(c.req.(*talentv3.DeleteSavedTalentRequest).TalentId, convey.ShouldEqual, 601)
			e.rec.set("DeleteSavedTalent", nil, errors.New("conn reset"))
			res, text := callTool(t, cli, "unsave_talent", map[string]any{"talent_id": 601}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "temporarily unavailable")
		})
	})
}

func TestCI_RecruiterMcp_ListInterviews(t *testing.T) {
	mockey.PatchConvey("Feature: list_interviews 返回面试日程（不含视频链接与联系电话）", t, func() {
		e, cli := recruiterEnv(t)
		at := timestamppb.New(time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC))
		e.rec.set("ListInterviews", &jobappv3.ListInterviewsResponse{TotalNum: 2, TotalPages: 1, Items: []*jobappv3.InterviewItem{
			{Id: 71, JobId: 31, JobTitle: "Accountant", JobseekerId: 601, InterviewedAt: at, StatusId: jobappv3.InterviewStatus_INTERVIEW_STATUS_ACCEPTED,
				Status: "Accepted", Address: "1 Raffles Place", FloorUnit: "#10-01", ContactPerson: "Rita", ContactPersonContactNum: "+6588887777",
				Jobseeker: &jobappv3.JobseekerSummary{FullName: "Ana Maria Santos", Email: "ana@example.com", Phone: "+639170000000"}},
			{Id: 72, JobseekerId: 602, StatusId: jobappv3.InterviewStatus_INTERVIEW_STATUS_CANCELLED, VideoCategory: "google_meet",
				VideoLink: "https://meet/secret", CancelledReason: "Schedule conflict", Address: "should be hidden"},
		}}, nil)
		mockey.PatchConvey("Scenario: 状态过滤与映射", func() {
			var out openv1.ListInterviewsResponse
			res, text := callTool(t, cli, "list_interviews", map[string]any{"states": []any{"INTERVIEW_STATE_ACCEPTED", "INTERVIEW_STATE_PENDING"}}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("ListInterviews")
			convey.So(c.req.(*jobappv3.ListInterviewsRequest).StatusIds, convey.ShouldResemble,
				[]jobappv3.InterviewStatus{jobappv3.InterviewStatus_INTERVIEW_STATUS_ACCEPTED, jobappv3.InterviewStatus_INTERVIEW_STATUS_PENDING_APPROVAL})
			assertRecruiterCtx(c)
			first, second := out.Interviews[0], out.Interviews[1]
			convey.So(first.TalentDisplayName, convey.ShouldEqual, "Ana S.")
			convey.So(first.State, convey.ShouldEqual, openv1.InterviewState_INTERVIEW_STATE_ACCEPTED)
			convey.So(first.Address, convey.ShouldEqual, "#10-01, 1 Raffles Place")
			convey.So(first.IsVideo, convey.ShouldBeFalse)
			convey.So(second.IsVideo, convey.ShouldBeTrue)
			convey.So(second.Address, convey.ShouldBeEmpty)
			convey.So(second.State, convey.ShouldEqual, openv1.InterviewState_INTERVIEW_STATE_CANCELLED)
			convey.So(second.CancelledReason, convey.ShouldEqual, "Schedule conflict")
			for _, s := range []string{"meet/secret", "6588887777", "ana@example.com", "639170000000", "Ana Maria Santos"} {
				convey.So(text, convey.ShouldNotContainSubstring, s)
			}
			convey.So(out.TotalSize, convey.ShouldEqual, 2)
		})
	})
}

func TestCI_RecruiterMcp_GetRecruitmentStats(t *testing.T) {
	mockey.PatchConvey("Feature: get_recruitment_stats 按周期返回招聘漏斗", t, func() {
		e, cli := recruiterEnv(t)
		e.rec.set("GetRecruitmentData", &recruiterv3.GetRecruitmentDataResponse{
			Today:     &recruiterv3.DayStats{ViewedTalent: 5, GreetedMe: 2, ExchangedCall: 1},
			Yesterday: &recruiterv3.DayStats{ViewedTalent: 3},
			Aggregate: &recruiterv3.DayStats{ViewedTalent: 50, InterviewCompleted: 4, ResumeReceived: 9},
		}, nil)
		mockey.PatchConvey("Scenario: 默认 TODAY 返回今天与昨天，不传 time_category_id", func() {
			var out openv1.GetRecruitmentStatsResponse
			res, _ := callTool(t, cli, "get_recruitment_stats", map[string]any{}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetRecruitmentData")
			req := c.req.(*recruiterv3.GetRecruitmentDataRequest)
			convey.So(req.TimeCategoryId, convey.ShouldEqual, 0)
			_, perr := time.Parse("2006-01-02", req.Date)
			convey.So(perr, convey.ShouldBeNil)
			assertRecruiterCtx(c)
			convey.So(out.Period, convey.ShouldEqual, openv1.StatsPeriod_STATS_PERIOD_TODAY)
			convey.So(out.Stats.ViewedTalents, convey.ShouldEqual, 5)
			convey.So(out.Stats.ContactsExchanged, convey.ShouldEqual, 1)
			convey.So(out.Yesterday.ViewedTalents, convey.ShouldEqual, 3)
		})
		mockey.PatchConvey("Scenario: THIS_MONTH 取聚合 + 指定职位", func() {
			var out openv1.GetRecruitmentStatsResponse
			callTool(t, cli, "get_recruitment_stats", map[string]any{"period": "STATS_PERIOD_THIS_MONTH", "job_id": 31}, &out)
			c, _ := e.rec.last("GetRecruitmentData")
			req := c.req.(*recruiterv3.GetRecruitmentDataRequest)
			convey.So(req.TimeCategoryId, convey.ShouldEqual, 3)
			convey.So(req.JobId, convey.ShouldEqual, 31)
			convey.So(out.Stats.ViewedTalents, convey.ShouldEqual, 50)
			convey.So(out.Stats.ResumesReceived, convey.ShouldEqual, 9)
			convey.So(out.Yesterday, convey.ShouldBeNil)
		})
	})
}

func TestCI_RecruiterMcp_GetSalaryInsight(t *testing.T) {
	mockey.PatchConvey("Feature: get_salary_insight 默认按所属公司国家站点查询", t, func() {
		e, cli := recruiterEnv(t)
		e.rec.set("SearchJobTitleLists", &configv3.SearchJobTitleListsResponse{JobTitles: []*configv3.ConfigJobTitleSearchItem{{Id: 88, Value: "Accountant"}}}, nil)
		e.rec.set("SearchSalary", &jobv3.SearchSalaryResponse{MedianData: 7000}, nil)
		mockey.PatchConvey("Scenario: 公司站点 SG ➔ 币种 SGD", func() {
			var out openv1.GetSalaryInsightResponse
			res, _ := callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "accountant"}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("SearchSalary")
			convey.So(c.req.(*jobv3.SearchSalaryRequest).GetFunctionJobTitleId(), convey.ShouldEqual, 88)
			convey.So(c.req.(*jobv3.SearchSalaryRequest).Source, convey.ShouldEqual, "mcp")
			assertRecruiterCtx(c)
			convey.So(out.CurrencyCode, convey.ShouldEqual, "SGD")
			convey.So(out.MedianMonthlySalary, convey.ShouldEqual, 7000)
		})
		mockey.PatchConvey("Scenario: country_code 显式切换站点", func() {
			var out openv1.GetSalaryInsightResponse
			callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "accountant", "location": "Makati", "country_code": "PH"}, &out)
			c, _ := e.rec.last("SearchSalary")
			convey.So(c.cinfo.CountryID, convey.ShouldEqual, 167)
			convey.So(c.req.(*jobv3.SearchSalaryRequest).GetJobLocationId(), convey.ShouldEqual, 201)
			convey.So(out.CurrencyCode, convey.ShouldEqual, "PHP")
		})
	})
}
