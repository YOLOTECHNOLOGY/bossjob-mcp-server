package tests

import (
	"strings"
	"testing"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobseekerv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/jobseeker/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	"github.com/bytedance/mockey"
	"github.com/golang-jwt/jwt/v5"
	"github.com/smartystreets/goconvey/convey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func jobFixture(id int64) *jobv3.JobDetailDTO {
	return &jobv3.JobDetailDTO{
		Id: id, JobTitle: "Golang Engineer", JobTypeId: 1, JobCountryId: 193, JobLocation: "Makati",
		CurrencyCode: "SGD", SalaryRangeFrom: "5000", SalaryRangeTo: "8000", LocalSalaryRangeValue: "S$5K - S$8K / month",
		SalaryType:      &jobv3.SalaryTypeDTO{Id: 1},
		XpLvl:           &jobv3.XpLvlDTO{Id: 2, Value: "1 - 3 Yrs"},
		Degree:          &jobv3.DegreeDTO{Value: "Bachelor"},
		WorkArrangement: &jobv3.WorkArrangementDTO{Id: 2},
		Company:         &jobv3.CompanyDTO{Id: 9, Name: "Acme", LogoUrl: "https://cdn/logo.png", IsVerify: true, CompanyUrl: "/company/acme-9"},
		Recruiter:       &jobv3.RecruiterDTO{Id: 7, FullName: "Rita", Email: "rita@acme.test", ResponseRate: 90, WorkExperience: &jobv3.WorkExperienceDTO{JobTitle: "HR"}},
		Skills:          []*jobv3.NamedItemDTO{{Name: "Go"}, {Value: "gRPC"}},
		JobDescription:  "Build services", JobRequirements: "3 years Go",
		JobUrl:      "/job/golang-engineer-" + strings.Repeat("1", 1),
		PublishedAt: timestamppb.Now(),
	}
}

func assertJobseekerCtx(c call, countryID int32) {
	convey.So(c.cinfo.UserID, convey.ShouldEqual, testUserID)
	convey.So(c.cinfo.RoleID, convey.ShouldEqual, 1)
	convey.So(c.cinfo.LanguageID, convey.ShouldEqual, 1)
	convey.So(c.cinfo.CountryID, convey.ShouldEqual, countryID)
	convey.So(c.cinfo.SID, convey.ShouldEqual, 0)
	convey.So(c.cinfo.DeviceType, convey.ShouldEqual, "mcp")
}

func TestCI_JobseekerMcp_SearchJobs(t *testing.T) {
	mockey.PatchConvey("Feature: search_jobs 将名称筛选解析后调用 job-go 搜索", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("SearchJobs", &jobv3.SearchJobsResponse{Jobs: []*jobv3.JobDetailDTO{jobFixture(11)}, TotalNum: 25, TotalPages: 3}, nil)

		mockey.PatchConvey("Scenario: 关键词 + 地点 + 经验 + 薪资下限 + 国家站点", func() {
			var out openv1.SearchJobsResponse
			res, text := callTool(t, cli, "search_jobs", map[string]any{
				"query": "golang", "locations": []any{"Makati"}, "country_code": "SG",
				"job_types": []any{"JOB_TYPE_FULL_TIME"}, "experience_levels": []any{"1 - 3 yrs"},
				"min_monthly_salary": 3000, "verified_company_only": true, "sort_order": "JOB_SORT_ORDER_RELEVANCE", "page_size": 5,
			}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, ok := e.rec.last("SearchJobs")
			convey.So(ok, convey.ShouldBeTrue)
			req := c.req.(*jobv3.SearchJobsRequest)
			convey.So(req.Query, convey.ShouldEqual, "golang")
			convey.So(req.JobLocations, convey.ShouldEqual, "Makati")
			convey.So(req.ExactSelectedLocation, convey.ShouldBeTrue)
			convey.So(req.JobTypeIds, convey.ShouldEqual, "1")
			convey.So(req.XpLvlIds, convey.ShouldEqual, "2")
			convey.So(req.SalaryFrom, convey.ShouldEqual, "3000")
			convey.So(req.SalaryTo, convey.ShouldEqual, "1000000000")
			convey.So(req.GetIsCompanyVerified(), convey.ShouldBeTrue)
			convey.So(req.Sort, convey.ShouldEqual, 2)
			convey.So(req.Page, convey.ShouldEqual, 1)
			convey.So(req.Size, convey.ShouldEqual, 5)
			convey.So(req.Source, convey.ShouldEqual, "mcp")
			assertJobseekerCtx(c, 193)
			cc, _ := e.rec.last("GetCountryConfig")
			convey.So(cc.req.(*configv3.GetCountryConfigRequest).CountryKey, convey.ShouldEqual, "sg")

			convey.So(out.TotalSize, convey.ShouldEqual, 25)
			convey.So(out.NextPageToken, convey.ShouldNotBeEmpty)
			convey.So(out.Jobs, convey.ShouldHaveLength, 1)
			j := out.Jobs[0]
			convey.So(j.JobId, convey.ShouldEqual, 11)
			convey.So(j.CountryCode, convey.ShouldEqual, "SG")
			convey.So(j.Salary.MinAmount, convey.ShouldEqual, "5000")
			convey.So(j.Salary.Period, convey.ShouldEqual, openv1.SalaryPeriod_SALARY_PERIOD_MONTHLY)
			convey.So(j.Salary.DisplayText, convey.ShouldEqual, "S$5K - S$8K / month")
			convey.So(j.WorkArrangement, convey.ShouldEqual, openv1.WorkArrangement_WORK_ARRANGEMENT_HYBRID)
			convey.So(j.Skills, convey.ShouldResemble, []string{"Go", "gRPC"})
			convey.So(j.JobUrl, convey.ShouldEqual, "https://dev.bossjob.com/job/golang-engineer-1")
			convey.So(j.Company.CompanyUrl, convey.ShouldEqual, "https://dev.bossjob.com/company/acme-9")
			convey.So(text, convey.ShouldNotContainSubstring, "rita@acme.test")

			mockey.PatchConvey("Scenario: next_page_token 翻到第 2 页", func() {
				_, _ = callTool(t, cli, "search_jobs", map[string]any{"query": "golang", "page_token": out.NextPageToken}, nil)
				c, _ := e.rec.last("SearchJobs")
				convey.So(c.req.(*jobv3.SearchJobsRequest).Page, convey.ShouldEqual, 2)
				convey.So(c.cinfo.CountryID, convey.ShouldEqual, 167)
			})
		})
		mockey.PatchConvey("Scenario: 只看远程职位走 SearchRemoteJobs", func() {
			e.rec.set("SearchRemoteJobs", &jobv3.SearchJobsResponse{}, nil)
			res, _ := callTool(t, cli, "search_jobs", map[string]any{"work_arrangements": []any{"WORK_ARRANGEMENT_REMOTE"}}, nil)
			convey.So(res.IsError, convey.ShouldBeFalse)
			convey.So(e.rec.count("SearchRemoteJobs"), convey.ShouldEqual, 1)
			convey.So(e.rec.count("SearchJobs"), convey.ShouldEqual, 0)
		})
		mockey.PatchConvey("Scenario: 非法参数返回工具错误且不调用下游", func() {
			for _, args := range []map[string]any{
				{"page_token": "not-a-token"},
				{"country_code": "XX"},
				{"page_size": 50},
				{"min_monthly_salary": 9000, "max_monthly_salary": 1000},
			} {
				res, _ := callTool(t, cli, "search_jobs", args, nil)
				convey.So(res.IsError, convey.ShouldBeTrue)
			}
			convey.So(e.rec.count("SearchJobs"), convey.ShouldEqual, 0)
		})
		mockey.PatchConvey("Scenario: 下游内部错误不泄露细节", func() {
			e.rec.set("SearchJobs", nil, status.Error(codes.Internal, "es cluster red: secret-host"))
			res, text := callTool(t, cli, "search_jobs", map[string]any{"query": "go"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldNotContainSubstring, "secret-host")
			convey.So(text, convey.ShouldContainSubstring, "temporarily unavailable")
		})
	})
}

func TestCI_JobseekerMcp_GetJob(t *testing.T) {
	mockey.PatchConvey("Feature: get_job 返回职位详情且不含联系方式", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		mockey.PatchConvey("Scenario: 职位存在", func() {
			e.rec.set("GetJobDetail", jobFixture(42), nil)
			var out openv1.GetJobResponse
			res, text := callTool(t, cli, "get_job", map[string]any{"job_id": "42"}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("GetJobDetail")
			convey.So(c.req.(*jobv3.GetJobDetailRequest).JobId, convey.ShouldEqual, 42)
			assertJobseekerCtx(c, 167)
			convey.So(out.Job.Description, convey.ShouldEqual, "Build services")
			convey.So(out.Job.Recruiter.DisplayName, convey.ShouldEqual, "Rita")
			convey.So(out.Job.Recruiter.JobTitle, convey.ShouldEqual, "HR")
			convey.So(text, convey.ShouldNotContainSubstring, "rita@acme.test")
		})
		mockey.PatchConvey("Scenario: 职位不存在透传 NotFound", func() {
			e.rec.set("GetJobDetail", nil, status.Error(codes.NotFound, "job not found"))
			res, text := callTool(t, cli, "get_job", map[string]any{"job_id": "43"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "job not found")
		})
		mockey.PatchConvey("Scenario: job_id 非法", func() {
			res, _ := callTool(t, cli, "get_job", map[string]any{"job_id": "0"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
		})
	})
}

func TestCI_JobseekerMcp_ListRecommendedJobs(t *testing.T) {
	mockey.PatchConvey("Feature: list_recommended_jobs 按求职偏好推荐", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("RecoJobs", &jobv3.SearchJobsResponse{Jobs: []*jobv3.JobDetailDTO{jobFixture(5)}, TotalPages: 1, IsFallback: true}, nil)
		var out openv1.ListRecommendedJobsResponse
		res, _ := callTool(t, cli, "list_recommended_jobs", map[string]any{"job_preference_id": "77", "page_size": 3}, &out)
		convey.So(res.IsError, convey.ShouldBeFalse)
		c, _ := e.rec.last("RecoJobs")
		req := c.req.(*jobv3.RecoJobsRequest)
		convey.So(req.JobPreferenceId, convey.ShouldEqual, 77)
		convey.So(req.SearchParams.Size, convey.ShouldEqual, 3)
		convey.So(req.SearchParams.Page, convey.ShouldEqual, 1)
		assertJobseekerCtx(c, 167)
		convey.So(out.IsFallback, convey.ShouldBeTrue)
		convey.So(out.NextPageToken, convey.ShouldBeEmpty)
		convey.So(out.Jobs, convey.ShouldHaveLength, 1)
	})
}

func TestCI_JobseekerMcp_GetMyProfile(t *testing.T) {
	mockey.PatchConvey("Feature: get_my_profile 返回不含联系方式的资料摘要", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("GetMyProfile", &jobseekerv3.GetMyProfileResponse{Profile: &jobseekerv3.JobseekerProfile{
			FullName: "Ana Cruz", Email: "ana@test", PhoneNum: "+63900", BirthDate: "1990-01-01", Location: "Manila",
			YearsOfExperience: "5 years", DegreeName: "Bachelor", ExpectedSalaryFrom: 40000, ExpectedSalaryTo: 60000, ExpectedSalaryCurrency: "PHP",
			JobseekingStatus: jobseekerv3.JobseekingStatus_JOBSEEKING_STATUS_ACTIVELY_LOOKING,
			WorkExperiences:  []*jobseekerv3.WorkExperience{{JobTitle: "Dev", CompanyName: "Foo", StartDate: "2020-03-01", IsCurrentJob: true}},
			Skills:           []*jobseekerv3.Skill{{Name: "Go"}},
			Resumes:          []*jobseekerv3.ResumeListItem{{Id: 1}},
		}}, nil)
		mockey.PatchConvey("Scenario: 资料、偏好与完成度齐全", func() {
			e.rec.set("ListJobPreferences", &jobseekerv3.ListJobPreferencesResponse{JobPreferences: []*jobseekerv3.JobPreference{{
				Id: 3, JobTitle: "Backend Engineer", LocationNames: []string{"Makati"}, JobTypes: []string{"Full-time"},
				WorkArrangement: jobseekerv3.WorkArrangement_WORK_ARRANGEMENT_REMOTE, SalaryFrom: 50000, SalaryCurrency: "PHP",
			}}}, nil)
			e.rec.set("GetProfileCompletion", &jobseekerv3.GetProfileCompletionResponse{Score: 80, Sections: []*jobseekerv3.ProfileCompletionSection{
				{Name: "Skills", IsComplete: true}, {Name: "Education", IsComplete: false},
			}}, nil)
			var out openv1.GetMyProfileResponse
			res, text := callTool(t, cli, "get_my_profile", map[string]any{}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			for _, secret := range []string{"ana@test", "+63900", "1990-01-01"} {
				convey.So(text, convey.ShouldNotContainSubstring, secret)
			}
			convey.So(out.Profile.FullName, convey.ShouldEqual, "Ana Cruz")
			convey.So(out.Profile.JobseekingStatus, convey.ShouldEqual, "Actively looking")
			convey.So(out.Profile.ExpectedSalary.MinAmount, convey.ShouldEqual, "40000")
			convey.So(out.Profile.WorkExperiences[0].StartDate, convey.ShouldEqual, "2020-03")
			convey.So(out.Profile.HasResume, convey.ShouldBeTrue)
			convey.So(out.JobPreferences[0].JobPreferenceId, convey.ShouldEqual, 3)
			convey.So(out.JobPreferences[0].JobTypes, convey.ShouldResemble, []openv1.JobType{openv1.JobType_JOB_TYPE_FULL_TIME})
			convey.So(out.JobPreferences[0].WorkArrangements, convey.ShouldResemble, []openv1.WorkArrangement{openv1.WorkArrangement_WORK_ARRANGEMENT_REMOTE})
			convey.So(out.Completion.Score, convey.ShouldEqual, 80)
			convey.So(out.Completion.IncompleteSections, convey.ShouldResemble, []string{"Education"})
			c, _ := e.rec.last("GetMyProfile")
			assertJobseekerCtx(c, 167)
		})
		mockey.PatchConvey("Scenario: 偏好与完成度失败时降级", func() {
			e.rec.set("ListJobPreferences", nil, status.Error(codes.Unavailable, "down"))
			e.rec.set("GetProfileCompletion", nil, status.Error(codes.Unavailable, "down"))
			var out openv1.GetMyProfileResponse
			res, _ := callTool(t, cli, "get_my_profile", map[string]any{}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			convey.So(out.Profile.FullName, convey.ShouldEqual, "Ana Cruz")
			convey.So(out.Completion, convey.ShouldBeNil)
		})
		mockey.PatchConvey("Scenario: 资料主接口失败则工具失败", func() {
			e.rec.set("GetMyProfile", nil, status.Error(codes.PermissionDenied, "not a jobseeker"))
			res, _ := callTool(t, cli, "get_my_profile", map[string]any{}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
		})
	})
}

func TestCI_JobseekerMcp_EvaluateJobMatch(t *testing.T) {
	mockey.PatchConvey("Feature: evaluate_job_match 透传匹配评估", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("JobMatchEvaluate", &jobseekerv3.JobMatchEvaluateResponse{IsMatch: true, Reasons: []string{"Go matched"}, Suggestions: []string{"Add Kubernetes"}}, nil)
		var out openv1.EvaluateJobMatchResponse
		res, _ := callTool(t, cli, "evaluate_job_match", map[string]any{"job_id": "8"}, &out)
		convey.So(res.IsError, convey.ShouldBeFalse)
		c, _ := e.rec.last("JobMatchEvaluate")
		convey.So(c.req.(*jobseekerv3.JobMatchEvaluateRequest).JobId, convey.ShouldEqual, 8)
		assertJobseekerCtx(c, 167)
		convey.So(out.IsMatch, convey.ShouldBeTrue)
		convey.So(out.Reasons, convey.ShouldResemble, []string{"Go matched"})
		convey.So(out.Suggestions, convey.ShouldResemble, []string{"Add Kubernetes"})
	})
}

func TestCI_JobseekerMcp_ListMyApplications(t *testing.T) {
	mockey.PatchConvey("Feature: list_my_applications 返回投递进度", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("ListAppliedJobs", &jobv3.ListAppliedJobsResponse{TotalNum: 1, TotalPages: 1, AppliedJobs: []*jobv3.JobApplicationListItem{{
			Id: 100, Status: "Interview", UpdatedAt: timestamppb.Now(), Job: &jobv3.JobApplicationJobSummary{Id: 11, JobTitle: "Dev", StatusKey: "active"},
		}}}, nil)
		var out openv1.ListMyApplicationsResponse
		res, _ := callTool(t, cli, "list_my_applications", map[string]any{}, &out)
		convey.So(res.IsError, convey.ShouldBeFalse)
		c, _ := e.rec.last("ListAppliedJobs")
		convey.So(c.req.(*jobv3.ListAppliedJobsRequest).Size, convey.ShouldEqual, 10)
		assertJobseekerCtx(c, 167)
		a := out.Applications[0]
		convey.So(a.ApplicationId, convey.ShouldEqual, 100)
		convey.So(a.JobId, convey.ShouldEqual, 11)
		convey.So(a.Status, convey.ShouldEqual, "Interview")
		convey.So(a.IsJobOpen, convey.ShouldBeTrue)
	})
}

func TestCI_JobseekerMcp_SavedJobs(t *testing.T) {
	mockey.PatchConvey("Feature: 收藏 / 取消收藏 / 收藏列表（幂等）", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		mockey.PatchConvey("Scenario: list_saved_jobs", func() {
			e.rec.set("ListSavedJobs", &jobv3.ListSavedJobsResponse{TotalNum: 2, TotalPages: 2, SavedJobs: []*jobv3.SavedJobItem{{
				JobId: 11, CreatedAt: timestamppb.Now(), Job: jobFixture(11),
			}}}, nil)
			var out openv1.ListSavedJobsResponse
			res, _ := callTool(t, cli, "list_saved_jobs", map[string]any{"page_size": 1}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			convey.So(out.SavedJobs[0].Job.JobId, convey.ShouldEqual, 11)
			convey.So(out.SavedJobs[0].Job.IsSaved, convey.ShouldBeTrue)
			convey.So(out.NextPageToken, convey.ShouldNotBeEmpty)
		})
		mockey.PatchConvey("Scenario: save_job 成功与重复收藏", func() {
			e.rec.set("CreateSavedJob", &jobv3.CreateSavedJobResponse{JobId: 11, CreatedAt: timestamppb.Now()}, nil)
			var out openv1.SaveJobResponse
			res, _ := callTool(t, cli, "save_job", map[string]any{"job_id": "11"}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			convey.So(out.JobId, convey.ShouldEqual, 11)
			convey.So(out.SaveTime, convey.ShouldNotBeNil)
			c, _ := e.rec.last("CreateSavedJob")
			convey.So(c.req.(*jobv3.CreateSavedJobRequest).GetSource(), convey.ShouldEqual, "mcp")
			assertJobseekerCtx(c, 167)

			e.rec.set("CreateSavedJob", nil, status.Error(codes.AlreadyExists, "already saved"))
			res, _ = callTool(t, cli, "save_job", map[string]any{"job_id": "11"}, nil)
			convey.So(res.IsError, convey.ShouldBeFalse)
		})
		mockey.PatchConvey("Scenario: unsave_job 未收藏也视为成功", func() {
			e.rec.set("DeleteSavedJob", nil, status.Error(codes.NotFound, "not saved"))
			var out openv1.UnsaveJobResponse
			res, _ := callTool(t, cli, "unsave_job", map[string]any{"job_id": "12"}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			convey.So(out.JobId, convey.ShouldEqual, 12)
			e.rec.set("DeleteSavedJob", nil, status.Error(codes.Internal, "db down"))
			res, _ = callTool(t, cli, "unsave_job", map[string]any{"job_id": "12"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
		})
	})
}

func TestCI_JobseekerMcp_GetSalaryInsight(t *testing.T) {
	mockey.PatchConvey("Feature: get_salary_insight 解析职位名称与地点后查询薪资", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, nil))
		e.rec.set("SearchJobTitleLists", &configv3.SearchJobTitleListsResponse{JobTitles: []*configv3.ConfigJobTitleSearchItem{
			{Id: 76, Value: "Senior Accountant"}, {Id: 77, Value: "Accountant"},
		}}, nil)
		e.rec.set("SearchSalary", &jobv3.SearchSalaryResponse{
			MedianData:     30000,
			PercentageList: []*jobv3.SearchSalaryPercentageItem{{SalaryRangeFrom: 20000, SalaryRangeTo: 30000, Percentage: "12.5%", Num: 4, SalaryRangeValue: "₱20K - ₱30K"}},
			AvgMonthList:   []*jobv3.SearchSalaryAvgMonthItem{{MonthNum: 1, SalaryAvg: 28000}},
		}, nil)

		mockey.PatchConvey("Scenario: 城市名命中 location_id", func() {
			var out openv1.GetSalaryInsightResponse
			res, _ := callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "accountant", "location": "cebu"}, &out)
			convey.So(res.IsError, convey.ShouldBeFalse)
			c, _ := e.rec.last("SearchSalary")
			req := c.req.(*jobv3.SearchSalaryRequest)
			convey.So(req.GetFunctionJobTitleId(), convey.ShouldEqual, 77)
			convey.So(req.GetJobLocationId(), convey.ShouldEqual, 101)
			convey.So(req.JobRegionId, convey.ShouldBeNil)
			assertJobseekerCtx(c, 167)
			convey.So(out.MatchedJobTitle, convey.ShouldEqual, "Accountant")
			convey.So(out.MatchedLocation, convey.ShouldEqual, "Cebu")
			convey.So(out.CurrencyCode, convey.ShouldEqual, "PHP")
			convey.So(out.MedianMonthlySalary, convey.ShouldEqual, 30000)
			convey.So(out.Distribution[0].Percentage, convey.ShouldEqual, 12.5)
			convey.So(out.Trend[0].Month, convey.ShouldEndWith, "-01")
		})
		mockey.PatchConvey("Scenario: 地区名命中 region_id", func() {
			_, _ = callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "Accountant", "location": "Metro Manila"}, nil)
			c, _ := e.rec.last("SearchSalary")
			req := c.req.(*jobv3.SearchSalaryRequest)
			convey.So(req.GetJobRegionId(), convey.ShouldEqual, 20)
			convey.So(req.JobLocationId, convey.ShouldBeNil)
		})
		mockey.PatchConvey("Scenario: 无法识别的地点或职位返回 NotFound", func() {
			res, text := callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "Accountant", "location": "Atlantis"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(text, convey.ShouldContainSubstring, "Atlantis")
			e.rec.set("SearchJobTitleLists", &configv3.SearchJobTitleListsResponse{}, nil)
			res, _ = callTool(t, cli, "get_salary_insight", map[string]any{"job_title": "Wizard"}, nil)
			convey.So(res.IsError, convey.ShouldBeTrue)
			convey.So(e.rec.count("SearchSalary"), convey.ShouldEqual, 0)
		})
	})
}

func TestCI_JobseekerMcp_TokenCountry(t *testing.T) {
	mockey.PatchConvey("Feature: 未指定 country_code 时使用 Token country_id 站点", t, func() {
		e := newEnv(t)
		cli := e.client(t, signToken(t, jwt.MapClaims{"country_id": 105}))
		e.rec.set("SearchJobs", &jobv3.SearchJobsResponse{}, nil)
		_, _ = callTool(t, cli, "search_jobs", map[string]any{"query": "sales", "experience_levels": []any{"No Experience"}}, nil)
		c, _ := e.rec.last("SearchJobs")
		assertJobseekerCtx(c, 105)
		cc, _ := e.rec.last("GetCountryConfig")
		convey.So(cc.req.(*configv3.GetCountryConfigRequest).CountryKey, convey.ShouldEqual, "jp")
		convey.So(cc.req.(*configv3.GetCountryConfigRequest).LanguageCode, convey.ShouldEqual, "en")
	})
}
