// Package recruiter 实现招聘者 MCP 工具（bossjob.open.v1.RecruiterMcpService）：
// 只通过 gRPC 调用 recruiter-go / job-go / talent-go / job-app-go / config-go，当前用户只取自 OAuth Token。
// 不提供任何扣额度或主动触达求职者的操作；人才数据统一脱敏（见 convert.go）。
package recruiter

import (
	"context"
	"strings"
	"time"

	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobappv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job_app/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	"github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1/openv1mcp"
	recruiterv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/recruiter/v3"
	talentv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/talent/v3"
	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/log"
	"github.com/samber/lo"
	"go.uber.org/dig"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"bossjob-mcp-server/domain/mcpkit"
	"bossjob-mcp-server/domain/oauthrs"
)

// source / device 透传给下游：埋点区分流量来源，并使人才详情与 Web 一致写入浏览记录。
const (
	source = "mcp"
	device = "mcp"
)

// Options 服务运行参数（由 DI 根据配置注入）。
type Options struct {
	WebBaseURL  string
	Timeout     time.Duration
	IdentityTTL time.Duration
}

// Deps 下游 gRPC 客户端、名称解析器与招聘者身份解析器。
type Deps struct {
	dig.In

	Options       Options
	Identity      IdentityResolver
	Resolver      mcpkit.Resolver
	Profiles      recruiterv3.RecruiterProfileServiceClient
	Stats         recruiterv3.RecruiterStatServiceClient
	JobManagement jobv3.JobManagementServiceClient
	JobSearch     jobv3.JobSearchServiceClient
	Talents       talentv3.TalentServiceClient
	TalentSearch  talentv3.TalentSearchServiceClient
	Interviews    jobappv3.RecruiterInterviewServiceClient
}

// Service 实现 openv1mcp.RecruiterMcpServiceServer。
type Service struct {
	d    Deps
	conv converter
	now  func() time.Time
}

var _ openv1mcp.RecruiterMcpServiceServer = (*Service)(nil)

// NewService 创建招聘者 MCP 工具服务。
func NewService(d Deps) *Service {
	return &Service{d: d, conv: converter{webBaseURL: d.Options.WebBaseURL}, now: time.Now}
}

// begin 校验身份、补全招聘者 SID 与公司国家站点，并设置单次调用超时。
// 身份上下文解析失败时降级为 Token 中的 country_id 且不带 SID（下游对 SID 为空有兜底），不阻断工具调用。
func (s *Service) begin(ctx context.Context) (context.Context, context.CancelFunc, error) {
	p := oauthrs.PrincipalFrom(ctx)
	if p == nil || p.UserID <= 0 || p.Role != oauthrs.RoleRecruiter {
		return ctx, func() {}, status.Error(codes.Unauthenticated, "sign in to Bossjob as a recruiter to use this tool")
	}
	cancel := func() {}
	if s.d.Options.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.d.Options.Timeout)
	}
	id, err := s.d.Identity.Resolve(ctx, p.UserID)
	if err != nil {
		log.WarnfCtx(ctx, "mcp recruiter identity lookup failed, fall back to token claims: uid=%d err=%v", p.UserID, err)
		return ctx, cancel, nil
	}
	enriched := *p
	enriched.SID = id.SID
	if id.CompanyCountryID > 0 {
		enriched.CountryID = id.CompanyCountryID
	}
	return oauthrs.WithIdentity(ctx, &enriched), cancel, nil
}

// GetMyRecruiterProfile 招聘者资料、所属公司、额度与可招聘状态。
func (s *Service) GetMyRecruiterProfile(ctx context.Context, req *openv1.GetMyRecruiterProfileRequest) (*openv1.GetMyRecruiterProfileResponse, error) {
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Profiles.GetMyProfile(ctx, &recruiterv3.GetMyProfileRequest{})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "recruiter profile", err)
	}
	return &openv1.GetMyRecruiterProfileResponse{Profile: s.conv.recruiterProfile(resp.GetProfile())}, nil
}

// ListMyJobs 当前招聘者发布的职位（固定 recruiter_id = 当前用户，不含同事职位）。
func (s *Service) ListMyJobs(ctx context.Context, req *openv1.ListMyJobsRequest) (*openv1.ListMyJobsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := mcpkit.DecodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	statuses := defaultJobStatuses
	if len(req.GetStatuses()) > 0 {
		statuses = strings.Join(lo.Uniq(lo.FilterMap(req.GetStatuses(), func(st openv1.ManagedJobStatus, _ int) (string, bool) {
			k, ok := jobStatusKey[st]
			return k, ok
		})), ",")
	}
	uid := oauthrs.PrincipalFrom(ctx).UserID
	resp, err := s.d.JobManagement.ManageJobList(ctx, &jobv3.ManageJobListRequest{
		Page: page, Size: mcpkit.PageSize(req.GetPageSize()), Query: req.GetQuery(), Status: statuses, RecruiterId: lo.ToPtr(uid),
	})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "job list", err)
	}
	return &openv1.ListMyJobsResponse{
		Jobs:          lo.Map(resp.GetJobs(), func(j *jobv3.ManageJobListItem, _ int) *openv1.ManagedJobSummary { return s.conv.managedJobSummary(j) }),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: mcpkit.NextPageToken(page, resp.GetTotalPages()),
		ActiveCount:   resp.GetTotalActiveJobs(),
		PendingCount:  resp.GetTotalPendingJobs(),
		ClosedCount:   resp.GetTotalClosedJobs(),
		DraftCount:    resp.GetTotalDraftJobs(),
	}, nil
}

// GetMyJob 职位详情（归属由 job-go 校验）。
func (s *Service) GetMyJob(ctx context.Context, req *openv1.GetMyJobRequest) (*openv1.GetMyJobResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	resp, err := s.d.JobManagement.ManageJobDetail(ctx, &jobv3.ManageJobDetailRequest{JobId: req.GetJobId()})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "job detail", err)
	}
	return &openv1.GetMyJobResponse{Job: s.conv.managedJob(resp)}, nil
}

// SearchTalents 人才库搜索：地点 / 经验 / 学历 / 到岗时间 / 语言名称经 config-go 解析为 ID，无法识别的名称忽略。
func (s *Service) SearchTalents(ctx context.Context, req *openv1.SearchTalentsRequest) (*openv1.SearchTalentsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, queryID, err := decodeQueryPage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	f, err := s.resolveFilters(ctx, req.GetLocations(), req.GetExperienceLevels(), req.GetDegrees(), req.GetNoticePeriods(), req.GetLanguages())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.TalentSearch.SearchTalent(ctx, &talentv3.SearchTalentRequest{
		CountryId: mcpkit.CurrentCountryID(ctx), Sort: searchSort(req.GetSortOrder()),
		Page: page, Size: mcpkit.PageSize(req.GetPageSize()), Source: source, Device: device,
		Query: req.GetQuery(), LocationIds: mcpkit.JoinIDs(f.locationIDs), RegionIds: mcpkit.JoinIDs(f.regionIDs),
		XpLvlIds: mcpkit.JoinIDs(f.xpLvlIDs), EduDegreeIds: mcpkit.JoinIDs(f.degreeIDs),
		NoticePeriodIds: mcpkit.JoinIDs(f.noticePeriodIDs), LanguageIds: mcpkit.JoinIDs(f.languageIDs),
		IsCurrentlyWorkHere: req.GetCurrentlyEmployedOnly(), IsOnline: req.GetOnlineOnly(),
		JobId: req.GetJobId(), QueryId: queryID,
	})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "talent search", err)
	}
	return &openv1.SearchTalentsResponse{
		Talents:       lo.Map(resp.GetTalents(), func(t *talentv3.TalentDetail, _ int) *openv1.TalentSummary { return talentSummaryFromDetail(t) }),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: nextQueryPageToken(page, resp.GetTotalPages(), resp.GetQueryId()),
	}, nil
}

// ListRecommendedTalents 职位推荐人才（职位归属由 talent-go 校验）。
func (s *Service) ListRecommendedTalents(ctx context.Context, req *openv1.ListRecommendedTalentsRequest) (*openv1.ListRecommendedTalentsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, queryID, err := decodeQueryPage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	f, err := s.resolveFilters(ctx, req.GetLocations(), req.GetExperienceLevels(), req.GetDegrees(), nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.d.TalentSearch.GetRecoTalent(ctx, &talentv3.GetRecoTalentRequest{
		JobId: req.GetJobId(), Sort: recoSort(req.GetSortOrder()), Page: page, Size: mcpkit.PageSize(req.GetPageSize()),
		Source: source, Device: device, LocationIds: mcpkit.JoinIDs(f.locationIDs),
		DegreeIds: mcpkit.JoinIDs(f.degreeIDs), XpLvlIds: mcpkit.JoinIDs(f.xpLvlIDs), QueryId: queryID,
	})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "talent recommendation", err)
	}
	return &openv1.ListRecommendedTalentsResponse{
		Talents:       lo.Map(resp.GetTalents(), func(t *talentv3.TalentDetail, _ int) *openv1.TalentSummary { return talentSummaryFromDetail(t) }),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: nextQueryPageToken(page, resp.GetTotalPages(), resp.GetQueryId()),
	}, nil
}

// GetTalent 人才详情：talent_id 为 users.id（GetTalentDetail；V2 需 jobseeker_sid），
// 传 source / device 使下游与 Web 一致记录浏览并受浏览频控。
func (s *Service) GetTalent(ctx context.Context, req *openv1.GetTalentRequest) (*openv1.GetTalentResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Talents.GetTalentDetail(ctx, &talentv3.GetTalentDetailRequest{
		TalentId: req.GetTalentId(), JobId: req.GetJobId(), Source: source, Device: device,
	})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "talent detail", err)
	}
	return &openv1.GetTalentResponse{Talent: talent(resp)}, nil
}

// ListJobCandidates 与招聘者或其职位有过互动的人才（已沟通 / 收藏我的职位 / 浏览我的职位）。
func (s *Service) ListJobCandidates(ctx context.Context, req *openv1.ListJobCandidatesRequest) (*openv1.ListJobCandidatesResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := mcpkit.DecodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	size := mcpkit.PageSize(req.GetPageSize())
	out := &openv1.ListJobCandidatesResponse{}
	var totalPages int32
	switch req.GetSource() {
	case openv1.CandidateSource_CANDIDATE_SOURCE_CHATTED:
		resp, err := s.d.Talents.GetChattedTalents(ctx, &talentv3.GetChattedTalentsRequest{Page: page, Size: size})
		if err != nil {
			return nil, mcpkit.UpstreamError(ctx, "chatted talent list", err)
		}
		out.Candidates = lo.Map(resp.GetChattedTalents(), func(it *talentv3.ChattedTalentItem, _ int) *openv1.JobCandidate {
			return &openv1.JobCandidate{Talent: cardOrID(it.GetTalent(), it.GetTalentId()), JobId: it.GetJobId(), InteractedAt: it.GetCreatedAt()}
		})
		out.TotalSize, totalPages = resp.GetTotalNum(), resp.GetTotalPages()
	case openv1.CandidateSource_CANDIDATE_SOURCE_SAVED_MY_JOB:
		resp, err := s.d.Talents.GetSavedMyJobTalents(ctx, &talentv3.GetSavedMyJobTalentsRequest{Page: page, Size: size, JobId: req.GetJobId()})
		if err != nil {
			return nil, mcpkit.UpstreamError(ctx, "candidate list", err)
		}
		out.Candidates = lo.Map(resp.GetSavedMyJobTalents(), func(it *talentv3.SavedMyJobTalentItem, _ int) *openv1.JobCandidate {
			return &openv1.JobCandidate{Talent: cardOrID(it.GetTalent(), it.GetTalentId()), JobId: it.GetJobId(), JobTitle: it.GetJob().GetJobTitle()}
		})
		out.TotalSize, totalPages = resp.GetTotalNum(), resp.GetTotalPages()
	default:
		resp, err := s.d.Talents.GetViewedMyJobTalents(ctx, &talentv3.GetViewedMyJobTalentsRequest{Page: page, Size: size, JobId: req.GetJobId()})
		if err != nil {
			return nil, mcpkit.UpstreamError(ctx, "candidate list", err)
		}
		out.Candidates = lo.Map(resp.GetViewedMyJobTalents(), func(it *talentv3.ViewedMyJobTalentItem, _ int) *openv1.JobCandidate {
			return &openv1.JobCandidate{Talent: cardOrID(it.GetTalent(), it.GetTalentId()), JobId: it.GetJobId(), JobTitle: it.GetJob().GetJobTitle()}
		})
		out.TotalSize, totalPages = resp.GetTotalNum(), resp.GetTotalPages()
	}
	out.NextPageToken = mcpkit.NextPageToken(page, totalPages)
	return out, nil
}

// ListSavedTalents 当前招聘者收藏的人才。
func (s *Service) ListSavedTalents(ctx context.Context, req *openv1.ListSavedTalentsRequest) (*openv1.ListSavedTalentsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := mcpkit.DecodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Talents.GetSavedTalents(ctx, &talentv3.GetSavedTalentsRequest{Page: page, Size: mcpkit.PageSize(req.GetPageSize())})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "saved talent list", err)
	}
	return &openv1.ListSavedTalentsResponse{
		SavedTalents: lo.Map(resp.GetSavedTalents(), func(it *talentv3.SavedTalentItem, _ int) *openv1.SavedTalent {
			t := cardOrID(it.GetTalent(), it.GetTalentId())
			t.IsSaved = true
			return &openv1.SavedTalent{Talent: t, JobId: it.GetJobId(), SaveTime: it.GetCreatedAt()}
		}),
		TotalSize:     resp.GetTotalNum(),
		NextPageToken: mcpkit.NextPageToken(page, resp.GetTotalPages()),
	}, nil
}

// SaveTalent 收藏人才（与 Web 一致：会通知求职者；幂等）。
func (s *Service) SaveTalent(ctx context.Context, req *openv1.SaveTalentRequest) (*openv1.SaveTalentResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	if _, err := s.d.Talents.SaveTalent(ctx, &talentv3.SaveTalentRequest{
		TalentId: req.GetTalentId(), JobId: req.GetJobId(), Source: source, Device: device,
	}); err != nil {
		return nil, mcpkit.UpstreamError(ctx, "save talent", err)
	}
	return &openv1.SaveTalentResponse{TalentId: req.GetTalentId()}, nil
}

// UnsaveTalent 取消收藏（幂等：talent-go 对未收藏返回 NotFound，视为成功）。
func (s *Service) UnsaveTalent(ctx context.Context, req *openv1.UnsaveTalentRequest) (*openv1.UnsaveTalentResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	if _, err := s.d.Talents.DeleteSavedTalent(ctx, &talentv3.DeleteSavedTalentRequest{TalentId: req.GetTalentId()}); err != nil &&
		status.Code(err) != codes.NotFound {
		return nil, mcpkit.UpstreamError(ctx, "unsave talent", err)
	}
	return &openv1.UnsaveTalentResponse{TalentId: req.GetTalentId()}, nil
}

// ListInterviews 当前招聘者的面试日程。
func (s *Service) ListInterviews(ctx context.Context, req *openv1.ListInterviewsRequest) (*openv1.ListInterviewsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	page, err := mcpkit.DecodePage(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.d.Interviews.ListInterviews(ctx, &jobappv3.ListInterviewsRequest{
		Page: page, Size: mcpkit.PageSize(req.GetPageSize()),
		StatusIds: lo.Uniq(lo.Map(req.GetStates(), func(st openv1.InterviewState, _ int) jobappv3.InterviewStatus { return jobappv3.InterviewStatus(st) })),
	})
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "interview list", err)
	}
	return &openv1.ListInterviewsResponse{
		Interviews:    lo.Map(resp.GetItems(), func(it *jobappv3.InterviewItem, _ int) *openv1.Interview { return interview(it) }),
		TotalSize:     int32(resp.GetTotalNum()),
		NextPageToken: mcpkit.NextPageToken(page, resp.GetTotalPages()),
	}, nil
}

// GetRecruitmentStats 招聘漏斗数据：TODAY 返回今天与昨天；其余周期为截至今天的累计。
func (s *Service) GetRecruitmentStats(ctx context.Context, req *openv1.GetRecruitmentStatsRequest) (*openv1.GetRecruitmentStatsResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	period := req.GetPeriod()
	if period == openv1.StatsPeriod_STATS_PERIOD_UNSPECIFIED {
		period = openv1.StatsPeriod_STATS_PERIOD_TODAY
	}
	pb := &recruiterv3.GetRecruitmentDataRequest{Date: s.now().Format("2006-01-02"), JobId: req.GetJobId()}
	if period != openv1.StatsPeriod_STATS_PERIOD_TODAY {
		pb.TimeCategoryId = int64(period)
	}
	resp, err := s.d.Stats.GetRecruitmentData(ctx, pb)
	if err != nil {
		return nil, mcpkit.UpstreamError(ctx, "recruitment stats", err)
	}
	out := &openv1.GetRecruitmentStatsResponse{Period: period}
	if period == openv1.StatsPeriod_STATS_PERIOD_TODAY {
		out.Stats, out.Yesterday = recruitmentStats(resp.GetToday()), recruitmentStats(resp.GetYesterday())
	} else {
		out.Stats = recruitmentStats(resp.GetAggregate())
	}
	return out, nil
}

// GetSalaryInsight 市场薪资洞察（与求职者工具共用 job-go SearchSalary），默认站点为所属公司国家。
func (s *Service) GetSalaryInsight(ctx context.Context, req *openv1.GetSalaryInsightRequest) (*openv1.GetSalaryInsightResponse, error) {
	if err := mcpkit.Validate(req); err != nil {
		return nil, err
	}
	ctx, cancel, err := s.begin(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	return mcpkit.SalaryInsight(ctx, s.d.JobSearch, s.d.Resolver, source, req)
}

type talentFilters struct {
	locationIDs, regionIDs, xpLvlIDs, degreeIDs, noticePeriodIDs, languageIDs []int64
}

func (s *Service) resolveFilters(ctx context.Context, locations, xpLvls, degrees, noticePeriods, languages []string) (talentFilters, error) {
	var f talentFilters
	var err error
	if f.locationIDs, f.regionIDs, err = s.d.Resolver.Locations(ctx, locations); err != nil {
		return f, err
	}
	if f.xpLvlIDs, err = s.d.Resolver.XpLvlIDs(ctx, xpLvls); err != nil {
		return f, err
	}
	if f.degreeIDs, err = s.d.Resolver.DegreeIDs(ctx, degrees); err != nil {
		return f, err
	}
	if f.noticePeriodIDs, err = s.d.Resolver.NoticePeriodIDs(ctx, noticePeriods); err != nil {
		return f, err
	}
	if f.languageIDs, err = s.d.Resolver.LanguageIDs(ctx, languages); err != nil {
		return f, err
	}
	return f, nil
}

func searchSort(o openv1.TalentSortOrder) talentv3.TalentSearchSort {
	switch o {
	case openv1.TalentSortOrder_TALENT_SORT_ORDER_RECENT_ACTIVE:
		return talentv3.TalentSearchSort_TALENT_SEARCH_SORT_ACTIVE
	case openv1.TalentSortOrder_TALENT_SORT_ORDER_RELEVANCE:
		return talentv3.TalentSearchSort_TALENT_SEARCH_SORT_RELEVANT
	}
	return talentv3.TalentSearchSort_TALENT_SEARCH_SORT_DEFAULT
}

func recoSort(o openv1.TalentSortOrder) talentv3.TalentRecoSort {
	if o == openv1.TalentSortOrder_TALENT_SORT_ORDER_RECENT_ACTIVE {
		return talentv3.TalentRecoSort_TALENT_RECO_SORT_ACTIVE
	}
	return talentv3.TalentRecoSort_TALENT_RECO_SORT_RECALL
}

func cardOrID(card *talentv3.TalentCardItem, talentID int64) *openv1.TalentSummary {
	if t := talentSummaryFromCard(card); t != nil {
		if t.TalentId == 0 {
			t.TalentId = talentID
		}
		return t
	}
	return &openv1.TalentSummary{TalentId: talentID, DisplayName: maskedName}
}
