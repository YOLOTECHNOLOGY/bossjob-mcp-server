// Package jobseeker 实现求职者 MCP 工具（bossjob.open.v1.JobseekerMcpService）：
// 只通过 gRPC 调用 job-go / jobseeker-go / config-go，当前用户只取自 OAuth Token。
package jobseeker

import (
	"context"
	"strconv"
	"strings"
	"time"

	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobseekerv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/jobseeker/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	"github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1/openv1mcp"
	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/log"
	"github.com/samber/lo"
	"go.uber.org/dig"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"bossjob-mcp-server/domain/oauthrs"
)

// source 透传给下游用于埋点区分流量来源。
const source = "mcp"

// noUpperSalary job-go 薪资过滤要求 from/to 成对出现，仅有下限时使用该上限。
const noUpperSalary = "1000000000"

// Options 服务运行参数（由 DI 根据配置注入）。
type Options struct {
	WebBaseURL string
	Timeout    time.Duration
}

// Deps 下游 gRPC 客户端与名称解析器。
type Deps struct {
	dig.In

	Options     Options
	Jobs        jobv3.JobServiceClient
	JobSearch   jobv3.JobSearchServiceClient
	Profiles    jobseekerv3.JobseekerProfileServiceClient
	Preferences jobseekerv3.JobseekerPreferenceServiceClient
	Resolver    Resolver
}

// Service 实现 openv1mcp.JobseekerMcpServiceServer。
type Service struct {
	d    Deps
	conv converter
}

var _ openv1mcp.JobseekerMcpServiceServer = (*Service)(nil)

// NewService 创建求职者 MCP 工具服务。
func NewService(d Deps) *Service {
	return &Service{d: d, conv: converter{webBaseURL: d.Options.WebBaseURL}}
}

// begin 校验身份并设置单次调用超时。
func (s *Service) begin(ctx context.Context) (context.Context, context.CancelFunc, error) {
	p := oauthrs.PrincipalFrom(ctx)
	if p == nil || p.UserID <= 0 || p.Role != oauthrs.RoleJobseeker {
		return ctx, func() {}, status.Error(codes.Unauthenticated, "sign in to Bossjob as a jobseeker to use this tool")
	}
	if s.d.Options.Timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, s.d.Options.Timeout)
		return ctx, cancel, nil
	}
	return ctx, func() {}, nil
}

// SearchJobs 职位搜索；只筛选远程职位时走 SearchRemoteJobs。
func (s *Service) SearchJobs(ctx context.Context, req *openv1.SearchJobsRequest) (*openv1.SearchJobsResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	if req.GetMinMonthlySalary() > 0 && req.GetMaxMonthlySalary() > 0 && req.GetMinMonthlySalary() > req.GetMaxMonthlySalary() {
		return nil, invalidArgument("min_monthly_salary must not exceed max_monthly_salary")
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := decodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	if ctx, err = withCountry(ctx, req.GetCountryCode()); err != nil {
		return nil, err
	}
	xpIDs, err := s.d.Resolver.XpLvlIDs(ctx, req.GetExperienceLevels())
	if err != nil {
		return nil, err
	}
	size := pageSize(req.GetPageSize())
	salaryFrom, salaryTo := salaryRange(req.GetMinMonthlySalary(), req.GetMaxMonthlySalary())
	jobTypeIDs := joinEnums(req.GetJobTypes())
	var verified *bool
	if req.GetVerifiedCompanyOnly() {
		verified = lo.ToPtr(true)
	}
	var salaryHidden *bool
	if req.GetExcludeHiddenSalary() {
		salaryHidden = lo.ToPtr(false)
	}

	var resp *jobv3.SearchJobsResponse
	if onlyRemote(req.GetWorkArrangements()) {
		resp, err = s.d.JobSearch.SearchRemoteJobs(ctx, &jobv3.SearchRemoteJobsRequest{
			Query: req.GetQuery(), Sort: int32(req.GetSortOrder()), Page: page, Size: size, Source: source,
			XpLvlIds: joinIDs(xpIDs), JobTypeIds: jobTypeIDs, SalaryFrom: salaryFrom, SalaryTo: salaryTo,
			IsCompanyVerified: verified, IsSalaryHidden: salaryHidden,
		})
	} else {
		resp, err = s.d.JobSearch.SearchJobs(ctx, &jobv3.SearchJobsRequest{
			Query: req.GetQuery(), Sort: int32(req.GetSortOrder()), Page: page, Size: size, Source: source,
			XpLvlIds: joinIDs(xpIDs), JobTypeIds: jobTypeIDs, SalaryFrom: salaryFrom, SalaryTo: salaryTo,
			JobLocations: strings.Join(req.GetLocations(), ","), ExactSelectedLocation: len(req.GetLocations()) > 0,
			WorkArrangementIds: joinEnums(req.GetWorkArrangements()), IsCompanyVerified: verified,
			IsSalaryHidden: salaryHidden, ExcludeNoSalary: req.GetExcludeHiddenSalary(),
		})
	}
	if err != nil {
		return nil, upstreamError(ctx, "job search", err)
	}
	return &openv1.SearchJobsResponse{
		Jobs:          lo.Map(resp.GetJobs(), func(j *jobv3.JobDetailDTO, _ int) *openv1.JobSummary { return s.conv.jobSummary(j) }),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: nextPageToken(page, resp.GetTotalPages()),
	}, nil
}

// GetJob 职位详情。
func (s *Service) GetJob(ctx context.Context, req *openv1.GetJobRequest) (*openv1.GetJobResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	d, err := s.d.Jobs.GetJobDetail(ctx, &jobv3.GetJobDetailRequest{JobId: req.GetJobId()})
	if err != nil {
		return nil, upstreamError(ctx, "job detail", err)
	}
	if d.GetId() == 0 {
		return nil, status.Error(codes.NotFound, "job not found or no longer available")
	}
	return &openv1.GetJobResponse{Job: s.conv.job(d)}, nil
}

// ListRecommendedJobs 按求职偏好推荐职位。
func (s *Service) ListRecommendedJobs(ctx context.Context, req *openv1.ListRecommendedJobsRequest) (*openv1.ListRecommendedJobsResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := decodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.JobSearch.RecoJobs(ctx, &jobv3.RecoJobsRequest{
		JobPreferenceId: req.GetJobPreferenceId(),
		SearchParams:    &jobv3.SearchJobsRequest{Page: page, Size: pageSize(req.GetPageSize()), Source: source},
	})
	if err != nil {
		return nil, upstreamError(ctx, "job recommendation", err)
	}
	return &openv1.ListRecommendedJobsResponse{
		Jobs:          lo.Map(resp.GetJobs(), func(j *jobv3.JobDetailDTO, _ int) *openv1.JobSummary { return s.conv.jobSummary(j) }),
		NextPageToken: nextPageToken(page, resp.GetTotalPages()),
		IsFallback:    resp.GetIsFallback(),
	}, nil
}

// GetMyProfile 个人资料摘要；偏好与完成度为 best-effort，失败时降级为空。
func (s *Service) GetMyProfile(ctx context.Context, req *openv1.GetMyProfileRequest) (*openv1.GetMyProfileResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	var (
		profileResp    *jobseekerv3.GetMyProfileResponse
		prefResp       *jobseekerv3.ListJobPreferencesResponse
		completionResp *jobseekerv3.GetProfileCompletionResponse
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var e error
		profileResp, e = s.d.Profiles.GetMyProfile(gctx, &jobseekerv3.GetMyProfileRequest{})
		return e
	})
	g.Go(func() error {
		var e error
		if prefResp, e = s.d.Preferences.ListJobPreferences(gctx, &jobseekerv3.ListJobPreferencesRequest{}); e != nil {
			log.WarnfCtx(gctx, "mcp ListJobPreferences degraded: %v", e)
		}
		return nil
	})
	g.Go(func() error {
		var e error
		if completionResp, e = s.d.Profiles.GetProfileCompletion(gctx, &jobseekerv3.GetProfileCompletionRequest{}); e != nil {
			log.WarnfCtx(gctx, "mcp GetProfileCompletion degraded: %v", e)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, upstreamError(ctx, "profile", err)
	}
	prefs := prefResp.GetJobPreferences()
	if len(prefs) == 0 {
		prefs = profileResp.GetProfile().GetJobPreferences()
	}
	out := &openv1.GetMyProfileResponse{
		Profile:        profile(profileResp.GetProfile()),
		JobPreferences: lo.Map(prefs, func(p *jobseekerv3.JobPreference, _ int) *openv1.JobPreference { return jobPreference(p) }),
	}
	if completionResp != nil {
		out.Completion = &openv1.ProfileCompletion{
			Score: completionResp.GetScore(),
			IncompleteSections: lo.FilterMap(completionResp.GetSections(), func(sec *jobseekerv3.ProfileCompletionSection, _ int) (string, bool) {
				return sec.GetName(), !sec.GetIsComplete()
			}),
		}
	}
	return out, nil
}

// EvaluateJobMatch 职位匹配评估。
func (s *Service) EvaluateJobMatch(ctx context.Context, req *openv1.EvaluateJobMatchRequest) (*openv1.EvaluateJobMatchResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Preferences.JobMatchEvaluate(ctx, &jobseekerv3.JobMatchEvaluateRequest{JobId: req.GetJobId()})
	if err != nil {
		return nil, upstreamError(ctx, "job match evaluation", err)
	}
	return &openv1.EvaluateJobMatchResponse{IsMatch: resp.GetIsMatch(), Reasons: resp.GetReasons(), Suggestions: resp.GetSuggestions()}, nil
}

// ListMyApplications 投递记录。
func (s *Service) ListMyApplications(ctx context.Context, req *openv1.ListMyApplicationsRequest) (*openv1.ListMyApplicationsResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := decodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Jobs.ListAppliedJobs(ctx, &jobv3.ListAppliedJobsRequest{Page: page, Size: pageSize(req.GetPageSize())})
	if err != nil {
		return nil, upstreamError(ctx, "application list", err)
	}
	return &openv1.ListMyApplicationsResponse{
		Applications: lo.Map(resp.GetAppliedJobs(), func(a *jobv3.JobApplicationListItem, _ int) *openv1.JobApplication {
			return &openv1.JobApplication{
				ApplicationId: a.GetId(),
				JobId:         a.GetJob().GetId(),
				JobTitle:      a.GetJob().GetJobTitle(),
				Status:        a.GetStatus(),
				IsJobOpen:     a.GetJob().GetStatusKey() == "active",
				UpdateTime:    a.GetUpdatedAt(),
			}
		}),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: nextPageToken(page, resp.GetTotalPages()),
	}, nil
}

// ListSavedJobs 收藏职位列表。
func (s *Service) ListSavedJobs(ctx context.Context, req *openv1.ListSavedJobsRequest) (*openv1.ListSavedJobsResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := decodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Jobs.ListSavedJobs(ctx, &jobv3.ListSavedJobsRequest{Page: page, Size: pageSize(req.GetPageSize())})
	if err != nil {
		return nil, upstreamError(ctx, "saved job list", err)
	}
	return &openv1.ListSavedJobsResponse{
		SavedJobs: lo.Map(resp.GetSavedJobs(), func(it *jobv3.SavedJobItem, _ int) *openv1.SavedJob {
			job := s.conv.jobSummary(it.GetJob())
			if job == nil {
				job = &openv1.JobSummary{JobId: it.GetJobId()}
			}
			if job.Company == nil {
				job.Company = s.conv.company(it.GetCompany())
			}
			job.IsSaved = true
			return &openv1.SavedJob{Job: job, SaveTime: it.GetCreatedAt()}
		}),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: nextPageToken(page, resp.GetTotalPages()),
	}, nil
}

// SaveJob 收藏职位（幂等：已收藏视为成功）。
func (s *Service) SaveJob(ctx context.Context, req *openv1.SaveJobRequest) (*openv1.SaveJobResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Jobs.CreateSavedJob(ctx, &jobv3.CreateSavedJobRequest{JobId: req.GetJobId(), Source: lo.ToPtr(source)})
	if status.Code(err) == codes.AlreadyExists {
		return &openv1.SaveJobResponse{JobId: req.GetJobId()}, nil
	}
	if err != nil {
		return nil, upstreamError(ctx, "save job", err)
	}
	return &openv1.SaveJobResponse{JobId: req.GetJobId(), SaveTime: resp.GetCreatedAt()}, nil
}

// UnsaveJob 取消收藏（幂等：未收藏视为成功）。
func (s *Service) UnsaveJob(ctx context.Context, req *openv1.UnsaveJobRequest) (*openv1.UnsaveJobResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	_, err = s.d.Jobs.DeleteSavedJob(ctx, &jobv3.DeleteSavedJobRequest{JobId: req.GetJobId()})
	if err != nil && status.Code(err) != codes.NotFound {
		return nil, upstreamError(ctx, "unsave job", err)
	}
	return &openv1.UnsaveJobResponse{JobId: req.GetJobId()}, nil
}

// GetSalaryInsight 薪资洞察：职位名称 / 地点经 config-go 解析为内部 ID 后调用 SearchSalary。
func (s *Service) GetSalaryInsight(ctx context.Context, req *openv1.GetSalaryInsightRequest) (*openv1.GetSalaryInsightResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	if ctx, err = withCountry(ctx, req.GetCountryCode()); err != nil {
		return nil, err
	}
	title, err := s.d.Resolver.JobTitle(ctx, req.GetJobTitle())
	if err != nil {
		return nil, err
	}
	if title == nil || title.ID <= 0 {
		return nil, status.Errorf(codes.NotFound, "no standard job title matches %q; try a more common title", req.GetJobTitle())
	}
	salaryReq := &salaryLookup{FunctionJobTitleID: title.ID}
	matchedLocation := ""
	if req.GetLocation() != "" {
		loc, err := s.d.Resolver.Location(ctx, req.GetLocation())
		if err != nil {
			return nil, err
		}
		if loc == nil {
			return nil, status.Errorf(codes.NotFound, "location %q is not a known city or region of this country site", req.GetLocation())
		}
		salaryReq.LocationID, salaryReq.RegionID, matchedLocation = loc.LocationID, loc.RegionID, loc.Name
	}
	resp, err := s.d.JobSearch.SearchSalary(ctx, salaryReq.toPB())
	if err != nil {
		return nil, upstreamError(ctx, "salary insight", err)
	}
	currency := ""
	if st, ok := siteByID(int64(currentCountryID(ctx))); ok {
		currency = st.Currency
	}
	return &openv1.GetSalaryInsightResponse{
		MatchedJobTitle:     title.Title,
		MatchedLocation:     matchedLocation,
		CurrencyCode:        currency,
		MedianMonthlySalary: resp.GetMedianData(),
		Distribution: lo.Map(resp.GetPercentageList(), func(p *jobv3.SearchSalaryPercentageItem, _ int) *openv1.SalaryBucket {
			return &openv1.SalaryBucket{
				MinAmount: int64(p.GetSalaryRangeFrom()), MaxAmount: int64(p.GetSalaryRangeTo()),
				Percentage: parsePercent(p.GetPercentage()), JobCount: p.GetNum(), DisplayText: p.GetSalaryRangeValue(),
			}
		}),
		Trend: salaryTrend(resp.GetAvgMonthList(), time.Now()),
	}, nil
}

type salaryLookup struct {
	FunctionJobTitleID int64
	LocationID         int64
	RegionID           int64
}

func (r *salaryLookup) toPB() *jobv3.SearchSalaryRequest {
	out := &jobv3.SearchSalaryRequest{FunctionJobTitleId: lo.ToPtr(r.FunctionJobTitleID), Source: source}
	if r.LocationID > 0 {
		out.JobLocationId = lo.ToPtr(r.LocationID)
	}
	if r.RegionID > 0 {
		out.JobRegionId = lo.ToPtr(r.RegionID)
	}
	return out
}

// salaryTrend 将 month_num（1-12，按时间先后）还原为 "YYYY-MM"：晚于当前月份的视为上一年。
func salaryTrend(items []*jobv3.SearchSalaryAvgMonthItem, now time.Time) []*openv1.MonthlySalaryAverage {
	return lo.FilterMap(items, func(it *jobv3.SearchSalaryAvgMonthItem, _ int) (*openv1.MonthlySalaryAverage, bool) {
		m := int(it.GetMonthNum())
		if m < 1 || m > 12 {
			return nil, false
		}
		year := now.Year()
		if m > int(now.Month()) {
			year--
		}
		return &openv1.MonthlySalaryAverage{Month: time.Date(year, time.Month(m), 1, 0, 0, 0, 0, time.UTC).Format("2006-01"), AverageSalary: it.GetSalaryAvg()}, true
	})
}

func parsePercent(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%")), 64)
	if err != nil {
		return 0
	}
	return v
}

func salaryRange(min, max int32) (string, string) {
	switch {
	case min > 0 && max > 0:
		return strconv.Itoa(int(min)), strconv.Itoa(int(max))
	case min > 0:
		return strconv.Itoa(int(min)), noUpperSalary
	case max > 0:
		return "0", strconv.Itoa(int(max))
	}
	return "", ""
}

func onlyRemote(was []openv1.WorkArrangement) bool {
	return len(was) > 0 && lo.EveryBy(was, func(w openv1.WorkArrangement) bool { return w == openv1.WorkArrangement_WORK_ARRANGEMENT_REMOTE })
}

func joinEnums[T ~int32](values []T) string {
	return strings.Join(lo.Map(lo.Uniq(values), func(v T, _ int) string { return strconv.Itoa(int(v)) }), ",")
}

func joinIDs(ids []int64) string {
	return strings.Join(lo.Map(lo.Uniq(ids), func(v int64, _ int) string { return strconv.FormatInt(v, 10) }), ",")
}
