package recruiter

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobappv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job_app/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	recruiterv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/recruiter/v3"
	talentv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/talent/v3"
	"github.com/samber/lo"

	"bossjob-mcp-server/domain/mcpkit"
)

// converter 将领域服务 DTO 转为精简的 MCP 输出。
//
// 人才数据流向第三方大模型，脱敏规则集中在此（红线，见 recruiter_mcp.proto 设计约束 5）：
//   - 姓名只输出 first name + last name 首字母（maskName），无论是否已解锁；
//   - 不映射 email / phone / birthdate / age / gender / avatar / 住址 / websites / 简历·视频·作品文件 URL；
//   - 自由文本（自我介绍、工作 / 教育描述）中的邮箱与电话号码替换为占位符（redactContacts）。
type converter struct {
	webBaseURL string
}

const maskedName = "Candidate"

// maskName 生成 "Wei L." 形式的脱敏姓名；缺少名字时返回通用称呼。
func maskName(first, last string) string {
	first = strings.TrimSpace(first)
	last = strings.TrimSpace(last)
	if first == "" && last == "" {
		return maskedName
	}
	if first == "" {
		first, last = last, ""
	}
	// first name 只取第一个词，避免把中间名 / 全名带出。
	if fields := strings.Fields(first); len(fields) > 0 {
		first = fields[0]
	}
	initial := ""
	if r, _ := utf8.DecodeRuneInString(last); r != utf8.RuneError && last != "" {
		initial = " " + string(unicode.ToUpper(r)) + "."
	}
	return first + initial
}

// maskFullName 只有展示名时：首个词为名，最后一个词为姓。
func maskFullName(full string) string {
	fields := strings.Fields(full)
	switch len(fields) {
	case 0:
		return maskedName
	case 1:
		return maskName(fields[0], "")
	default:
		return maskName(fields[0], fields[len(fields)-1])
	}
}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// 至少 8 位数字（允许空格、横线、括号、点分隔，可带 +），覆盖常见手机 / 座机格式。
	phonePattern = regexp.MustCompile(`\+?\(?\d[\d\s().\-]{6,}\d`)
	// 年份区间（2012-2016）与日期（2020.01.15 / 2020-01-15）不是电话，避免误删经历时间。
	dateLikePattern = regexp.MustCompile(`^(\d{4}\s*[-–.]\s*\d{4}|\d{4}[-./]\d{1,2}[-./]\d{1,2})$`)
)

// redactContacts 去除自由文本中的邮箱与电话号码。
func redactContacts(s string) string {
	if s == "" {
		return s
	}
	s = emailPattern.ReplaceAllString(s, "[email removed]")
	return phonePattern.ReplaceAllStringFunc(s, func(m string) string {
		digits := 0
		for _, r := range m {
			if unicode.IsDigit(r) {
				digits++
			}
		}
		if digits < 8 || dateLikePattern.MatchString(strings.TrimSpace(m)) {
			return m
		}
		return "[phone removed]"
	})
}

func jobseekingStatusText(s talentv3.JobseekingStatus) string {
	switch s {
	case talentv3.JobseekingStatus_JOBSEEKING_STATUS_ACTIVELY_LOOKING:
		return "Actively looking"
	case talentv3.JobseekingStatus_JOBSEEKING_STATUS_PASSIVELY_LOOKING:
		return "Open to opportunities"
	case talentv3.JobseekingStatus_JOBSEEKING_STATUS_NOT_LOOKING:
		return "Not looking"
	}
	return ""
}

func cardTags(items []*talentv3.CardTagItem) []string {
	return lo.FilterMap(items, func(t *talentv3.CardTagItem, _ int) (string, bool) { return t.GetT(), t.GetT() != "" })
}

func nationality(n *talentv3.TalentUserNationality) string {
	if n == nil {
		return ""
	}
	if code := mcpkit.CountryCode(n.GetCountryId()); code != "" {
		return code
	}
	return n.GetCountryKey()
}

// talentSummaryFromDetail 搜索 / 推荐 / 详情返回的 TalentDetail ➔ TalentSummary。
func talentSummaryFromDetail(d *talentv3.TalentDetail) *openv1.TalentSummary {
	if d == nil {
		return nil
	}
	xp := d.GetWorkExperience()
	return &openv1.TalentSummary{
		TalentId:            d.GetId(),
		DisplayName:         maskName(d.GetFirstName(), d.GetLastName()),
		CurrentJobTitle:     mcpkit.FirstNonEmpty(d.GetCurrentJobTitle(), xp.GetJobTitle()),
		CurrentCompany:      mcpkit.FirstNonEmpty(d.GetCurrentCompanyName(), xp.GetCompany()),
		IsCurrentlyEmployed: d.GetIsCurrentlyWorkHere() || xp.GetIsCurrentlyWorkHere(),
		ExperienceLevel:     mcpkit.FirstNonEmpty(d.GetXpLvl(), yearsText(d.GetYearsOfExperience())),
		HighestDegree:       mcpkit.FirstNonEmpty(d.GetDegreeValue(), d.GetEducation().GetDegreeValue()),
		School:              mcpkit.FirstNonEmpty(d.GetEduSchoolName(), d.GetEducation().GetSchool()),
		Location:            d.GetLocationValue(),
		Nationality:         nationality(d.GetUserNationality()),
		ExpectedSalary:      expectedSalary(d.GetSalaryType(), d),
		NoticePeriod:        d.GetNoticePeriodValue(),
		JobseekingStatus:    jobseekingStatusText(d.GetJobseekingStatusId()),
		Skills:              d.GetSkills(),
		Tags:                cardTags(d.GetCardTags()),
		IsFreshGraduate:     d.GetIsFreshGraduate(),
		IsOnline:            d.GetIsOnline(),
		ActiveStatus:        d.GetActiveStatus().GetValue(),
		LastActiveAt:        d.GetLastActiveAt(),
		IsSaved:             d.GetIsSavedTalent(),
		IsChatted:           d.GetIsChattedTalent(),
		IsUnlocked:          d.GetIsUnlockedTalent(),
	}
}

// talentSummaryFromCard 候选人 / 收藏列表的 TalentCardItem ➔ TalentSummary。
func talentSummaryFromCard(d *talentv3.TalentCardItem) *openv1.TalentSummary {
	if d == nil {
		return nil
	}
	xp := d.GetWorkExperience()
	return &openv1.TalentSummary{
		TalentId:            d.GetId(),
		DisplayName:         maskName(d.GetFirstName(), d.GetLastName()),
		CurrentJobTitle:     xp.GetJobTitle(),
		CurrentCompany:      xp.GetCompany(),
		IsCurrentlyEmployed: xp.GetIsCurrentlyWorkHere(),
		ExperienceLevel:     yearsText(int64(d.GetYearsOfExperience())),
		HighestDegree:       d.GetEducation().GetDegreeValue(),
		School:              d.GetEducation().GetSchool(),
		Location:            d.GetLatestPreference().GetLocation(),
		Nationality:         nationality(d.GetUserNationality()),
		ExpectedSalary: mcpkit.FirstNonEmpty(d.GetLocalSalaryRangeValue(), d.GetSalaryRangeValue(),
			d.GetLocalDailySalaryRangeValue(), d.GetDailySalaryRangeValue(),
			d.GetLocalHourlySalaryRangeValue(), d.GetHourlySalaryRangeValue()),
		NoticePeriod:    d.GetNoticePeriodValue(),
		Skills:          d.GetSkills(),
		Tags:            cardTags(d.GetCardTags()),
		IsFreshGraduate: d.GetIsFreshGraduate(),
		IsOnline:        d.GetIsOnline(),
		ActiveStatus:    d.GetActiveStatus().GetValue(),
		LastActiveAt:    d.GetLastActiveAt(),
		IsSaved:         d.GetIsSavedTalent(),
		IsChatted:       d.GetIsChattedTalent(),
		IsUnlocked:      d.GetIsUnlockedTalent(),
	}
}

func yearsText(years int64) string {
	switch {
	case years <= 0:
		return ""
	case years == 1:
		return "1 year"
	default:
		return fmt.Sprintf("%d years", years)
	}
}

// expectedSalary 按薪资周期取本地化期望薪资文本。
func expectedSalary(salaryType string, d *talentv3.TalentDetail) string {
	monthly := mcpkit.FirstNonEmpty(d.GetLocalSalaryRangeValue(), d.GetSalaryRangeValue())
	daily := mcpkit.FirstNonEmpty(d.GetLocalDailySalaryRangeValue(), d.GetDailySalaryRangeValue())
	hourly := mcpkit.FirstNonEmpty(d.GetLocalHourlySalaryRangeValue(), d.GetHourlySalaryRangeValue())
	switch strings.ToLower(salaryType) {
	case "daily", "2":
		return mcpkit.FirstNonEmpty(daily, monthly, hourly)
	case "hourly", "3":
		return mcpkit.FirstNonEmpty(hourly, monthly, daily)
	}
	return mcpkit.FirstNonEmpty(monthly, daily, hourly)
}

// talent 人才详情：在 TalentSummary 基础上补充经历、教育、语言、证书与求职期望（长文本已去联系方式）。
func talent(d *talentv3.TalentDetail) *openv1.Talent {
	if d == nil {
		return nil
	}
	ym := d.GetWorkingYearsMonths()
	total := ""
	if ym.GetYears() > 0 || ym.GetMonths() > 0 {
		total = strings.TrimSpace(lo.Ternary(ym.GetYears() > 0, yearsText(ym.GetYears()), "") + " " +
			lo.Ternary(ym.GetMonths() > 0, fmt.Sprintf("%d months", ym.GetMonths()), ""))
	}
	works := d.GetWorkExperiences()
	if len(works) == 0 && d.GetWorkExperience() != nil {
		works = []*talentv3.TalentWorkXp{d.GetWorkExperience()}
	}
	edus := d.GetEducations()
	if len(edus) == 0 && d.GetEducation() != nil {
		edus = []*talentv3.TalentEducation{d.GetEducation()}
	}
	prefs := d.GetJobPreferences()
	if len(prefs) == 0 && d.GetJobPreference() != nil {
		prefs = []*talentv3.TalentPreference{d.GetJobPreference()}
	}
	return &openv1.Talent{
		Summary:         talentSummaryFromDetail(d),
		About:           redactContacts(d.GetDescription()),
		TotalExperience: total,
		WorkExperiences: lo.Map(works, func(w *talentv3.TalentWorkXp, _ int) *openv1.WorkExperience {
			return &openv1.WorkExperience{
				JobTitle: w.GetJobTitle(), CompanyName: w.GetCompany(),
				StartDate: w.GetWorkingPeriodFrom(), EndDate: lo.Ternary(w.GetIsCurrentlyWorkHere(), "", w.GetWorkingPeriodTo()),
				IsCurrentJob: w.GetIsCurrentlyWorkHere(), Description: redactContacts(w.GetDescription()),
			}
		}),
		Educations: lo.Map(edus, func(e *talentv3.TalentEducation, _ int) *openv1.Education {
			return &openv1.Education{
				School: e.GetSchool(), Degree: e.GetDegreeValue(), FieldOfStudy: e.GetFieldOfStudy(),
				StartDate: e.GetStudyPeriodFrom(), EndDate: lo.Ternary(e.GetIsCurrentlyStudying(), "", e.GetStudyPeriodTo()),
				IsCurrentStudy: e.GetIsCurrentlyStudying(),
			}
		}),
		Languages: lo.FilterMap(d.GetLanguages(), func(l *talentv3.TalentLanguage, _ int) (string, bool) {
			if l.GetLanguageValue() == "" {
				return "", false
			}
			if l.GetProficiencyValue() == "" {
				return l.GetLanguageValue(), true
			}
			return l.GetLanguageValue() + " (" + l.GetProficiencyValue() + ")", true
		}),
		Certifications: lo.FilterMap(d.GetLicenseCertifications(), func(c *talentv3.TalentLicenseCertification, _ int) (string, bool) {
			return c.GetName(), c.GetName() != ""
		}),
		JobPreferences: lo.Map(prefs, func(p *talentv3.TalentPreference, _ int) *openv1.TalentPreference {
			out := &openv1.TalentPreference{
				JobTitle: p.GetJobTitle(),
				ExpectedSalary: mcpkit.FirstNonEmpty(p.GetLocalSalaryRangeValue(), p.GetSalaryRangeValue(),
					p.GetLocalDailySalaryRangeValue(), p.GetDailySalaryRangeValue(),
					p.GetLocalHourlySalaryRangeValue(), p.GetHourlySalaryRangeValue()),
			}
			if p.GetLocation() != "" {
				out.Locations = []string{p.GetLocation()}
			}
			if wa := openv1.WorkArrangement(p.GetWorkArrangementId()); wa != openv1.WorkArrangement_WORK_ARRANGEMENT_UNSPECIFIED {
				if _, ok := openv1.WorkArrangement_name[int32(wa)]; ok {
					out.WorkArrangements = []openv1.WorkArrangement{wa}
				}
			}
			return out
		}),
		HasResume:      d.GetResume().GetId() > 0,
		HasVideoResume: len(d.GetVideoResumes()) > 0,
	}
}

// ============================================================================
// 招聘者资料
// ============================================================================

func (c converter) recruiterProfile(p *recruiterv3.RecruiterMyProfile) *openv1.RecruiterProfile {
	if p == nil {
		return nil
	}
	ready, reason := readiness(p)
	out := &openv1.RecruiterProfile{
		FullName:       mcpkit.FirstNonEmpty(p.GetFullName(), strings.TrimSpace(p.GetFirstName()+" "+p.GetLastName())),
		JobTitle:       p.GetWorkExperience().GetJobTitle(),
		IsReadyToHire:  ready,
		NotReadyReason: reason,
		ActiveJobCount: p.GetActiveJobNum(),
		ResponseRate:   p.GetResponseRate(),
		ResponseTime:   p.GetResponseTime(),
		LastActiveAt:   p.GetLastActiveAt(),
	}
	if co := p.GetCompany(); co != nil && co.GetId() > 0 {
		out.Company = &openv1.CompanySummary{
			CompanyId: co.GetId(), Name: co.GetName(), LogoUrl: co.GetLogoUrl(), Industry: co.GetIndustry(),
			CompanySize: co.GetCompanySize(), FinancingStage: co.GetFinancingStage(), IsVerified: co.GetIsVerify(),
			CompanyUrl: mcpkit.AbsURL(c.webBaseURL, co.GetCompanyUrl()),
		}
	}
	if cr := p.GetAvailableCredits(); cr != nil {
		out.Credits = &openv1.RecruiterCredits{
			JobPost: cr.GetJobPost(), UnlockTalent: cr.GetUnlockTalent(), FeaturedJob: cr.GetFeatureJob(), UrgentJob: cr.GetUrgentJob(),
		}
	}
	if sub := p.GetSubscription(); sub != nil {
		out.SubscriptionPlan = sub.GetName()
	}
	return out
}

// readiness 招聘者是否已加入并通过认证的公司（可发布职位、搜索人才）。
func readiness(p *recruiterv3.RecruiterMyProfile) (bool, string) {
	xp, co := p.GetWorkExperience(), p.GetCompany()
	if xp.GetCompanyId() <= 0 && co.GetId() <= 0 {
		return false, "The recruiter has not joined a company yet; join or create a company in Bossjob first"
	}
	switch strings.ToLower(xp.GetStatusKey()) {
	case "", "active":
	case "rejected", "deleted":
		return false, "The request to join the company was rejected; update the company in Bossjob"
	default:
		return false, "The request to join the company is pending review in Bossjob"
	}
	if co != nil && co.GetId() > 0 && !co.GetIsVerify() {
		return false, "Company verification is pending; complete it in Bossjob"
	}
	return true, ""
}

// ============================================================================
// 职位（只读）
// ============================================================================

var jobStatusByKey = map[string]openv1.ManagedJobStatus{
	"active":                       openv1.ManagedJobStatus_MANAGED_JOB_STATUS_ACTIVE,
	"pending":                      openv1.ManagedJobStatus_MANAGED_JOB_STATUS_PENDING,
	"company_pending_verification": openv1.ManagedJobStatus_MANAGED_JOB_STATUS_PENDING,
	"change_required":              openv1.ManagedJobStatus_MANAGED_JOB_STATUS_CHANGE_REQUIRED,
	"rejected":                     openv1.ManagedJobStatus_MANAGED_JOB_STATUS_REJECTED,
	"closed":                       openv1.ManagedJobStatus_MANAGED_JOB_STATUS_CLOSED,
	"draft":                        openv1.ManagedJobStatus_MANAGED_JOB_STATUS_DRAFT,
	"expired":                      openv1.ManagedJobStatus_MANAGED_JOB_STATUS_EXPIRED,
}

var jobStatusKey = map[openv1.ManagedJobStatus]string{
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_ACTIVE:          "active",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_PENDING:         "pending",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_CHANGE_REQUIRED: "change_required",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_REJECTED:        "rejected",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_CLOSED:          "closed",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_DRAFT:           "draft",
	openv1.ManagedJobStatus_MANAGED_JOB_STATUS_EXPIRED:         "expired",
}

// defaultJobStatuses list_my_jobs 未指定状态时：除草稿与过期外的全部状态。
const defaultJobStatuses = "active,pending,change_required,rejected,closed"

func jobStatus(key string) openv1.ManagedJobStatus {
	return jobStatusByKey[strings.ToLower(strings.TrimSpace(key))]
}

func currencyOf(countryID int64) string {
	if st, ok := mcpkit.SiteByID(countryID); ok {
		return st.Currency
	}
	return ""
}

func salaryPeriod(id int64) openv1.SalaryPeriod {
	if _, ok := openv1.SalaryPeriod_name[int32(id)]; ok && id > 0 {
		return openv1.SalaryPeriod(id)
	}
	return openv1.SalaryPeriod_SALARY_PERIOD_MONTHLY
}

type salaryFields struct {
	periodID                                       int64
	countryID                                      int64
	hidden                                         bool
	monthlyFrom, monthlyTo                         float64
	dailyFrom, dailyTo                             float64
	hourlyFrom, hourlyTo                           float64
	annualFrom, annualTo                           float64
	monthlyText, dailyText, hourlyText, annualText string
}

func (f salaryFields) salary() *openv1.Salary {
	s := &openv1.Salary{CurrencyCode: currencyOf(f.countryID), Period: salaryPeriod(f.periodID), IsHidden: f.hidden}
	var from, to float64
	switch s.Period {
	case openv1.SalaryPeriod_SALARY_PERIOD_DAILY:
		from, to, s.DisplayText = f.dailyFrom, f.dailyTo, f.dailyText
	case openv1.SalaryPeriod_SALARY_PERIOD_HOURLY:
		from, to, s.DisplayText = f.hourlyFrom, f.hourlyTo, f.hourlyText
	case openv1.SalaryPeriod_SALARY_PERIOD_ANNUALLY:
		from, to, s.DisplayText = f.annualFrom, f.annualTo, f.annualText
	default:
		from, to, s.DisplayText = f.monthlyFrom, f.monthlyTo, f.monthlyText
	}
	if from > 0 {
		s.MinAmount = trimFloat(from)
	}
	if to > 0 {
		s.MaxAmount = trimFloat(to)
	}
	return s
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func (c converter) managedJobSummary(d *jobv3.ManageJobListItem) *openv1.ManagedJobSummary {
	if d == nil {
		return nil
	}
	return &openv1.ManagedJobSummary{
		JobId:    d.GetId(),
		Title:    d.GetJobTitle(),
		Status:   jobStatus(d.GetStatusKey()),
		Location: d.GetJobLocation(),
		Salary: salaryFields{
			periodID: d.GetSalaryType().GetId(), countryID: d.GetCountryId(), hidden: d.GetIsSalaryHidden(),
			monthlyFrom: d.GetSalaryRangeFrom(), monthlyTo: d.GetSalaryRangeTo(),
			monthlyText: mcpkit.FirstNonEmpty(d.GetLocalSalaryRangeValue(), d.GetSalaryRangeValue()),
			dailyText:   mcpkit.FirstNonEmpty(d.GetLocalDailySalaryRangeValue(), d.GetDailySalaryRangeValue()),
			hourlyText:  mcpkit.FirstNonEmpty(d.GetLocalHourlySalaryRangeValue(), d.GetHourlySalaryRangeValue()),
			annualText:  mcpkit.FirstNonEmpty(d.GetLocalAnnuallySalaryRangeValue(), d.GetAnnuallySalaryRangeValue()),
		}.salary(),
		WorkArrangement: workArrangement(d.GetWorkArrangement().GetId()),
		ExperienceLevel: d.GetXpLvl(),
		Degree:          d.GetDegree(),
		IsFeatured:      d.GetIsFeatured(),
		IsUrgent:        d.GetIsUrgent(),
		ViewCount:       d.GetNoOfViews(),
		ApplicantCount:  d.GetJobApplicationCount().GetTotal(),
		ReviewFeedback:  mcpkit.FirstNonEmpty(d.GetChangeRequiredReason(), d.GetRejectedReason()),
		PublishedAt:     d.GetPublishedAt(),
		CreatedAt:       d.GetCreatedAt(),
		JobUrl:          mcpkit.AbsURL(c.webBaseURL, d.GetJobUrl()),
	}
}

func (c converter) managedJob(d *jobv3.ManageJobDetailResponse) *openv1.ManagedJob {
	if d == nil {
		return nil
	}
	return &openv1.ManagedJob{
		Summary: &openv1.ManagedJobSummary{
			JobId:    d.GetId(),
			Title:    d.GetJobTitle(),
			Status:   jobStatus(d.GetStatusKey()),
			Location: d.GetJobLocation(),
			Salary: salaryFields{
				periodID: d.GetSalaryTypeId(), countryID: d.GetCountryId(), hidden: d.GetIsSalaryHidden(),
				monthlyFrom: d.GetSalaryRangeFrom(), monthlyTo: d.GetSalaryRangeTo(),
				dailyFrom: d.GetDailySalaryRangeFrom(), dailyTo: d.GetDailySalaryRangeTo(),
				hourlyFrom: d.GetHourlySalaryRangeFrom(), hourlyTo: d.GetHourlySalaryRangeTo(),
				annualFrom: d.GetAnnuallySalaryRangeFrom(), annualTo: d.GetAnnuallySalaryRangeTo(),
				monthlyText: mcpkit.FirstNonEmpty(d.GetLocalSalaryRangeValue(), d.GetSalaryRangeValue()),
				dailyText:   mcpkit.FirstNonEmpty(d.GetLocalDailySalaryRangeValue(), d.GetDailySalaryRangeValue()),
				hourlyText:  mcpkit.FirstNonEmpty(d.GetLocalHourlySalaryRangeValue(), d.GetHourlySalaryRangeValue()),
				annualText:  mcpkit.FirstNonEmpty(d.GetLocalAnnuallySalaryRangeValue(), d.GetAnnuallySalaryRangeValue()),
			}.salary(),
			WorkArrangement: workArrangement(lo.Ternary(d.GetWorkArrangementId() > 0, d.GetWorkArrangementId(), d.GetWorkArrangement().GetId())),
			ExperienceLevel: d.GetXpLvl(),
			Degree:          d.GetDegree(),
			IsFeatured:      d.GetIsFeatured(),
			IsUrgent:        d.GetIsUrgent(),
			ViewCount:       int32(d.GetNoOfViews()),
			ReviewFeedback:  mcpkit.FirstNonEmpty(d.GetChangeRequiredReason(), d.GetRejectedReason()),
			PublishedAt:     d.GetPublishedAt(),
			CreatedAt:       d.GetCreatedAt(),
			JobUrl:          mcpkit.AbsURL(c.webBaseURL, d.GetJobUrl()),
		},
		JobType:          jobType(d.GetJobTypeId()),
		JobFunction:      d.GetFunctionJobTitle(),
		Skills:           d.GetJobSkills(),
		Benefits:         names(d.GetJobBenefits()),
		Languages:        lo.FilterMap(d.GetLanguages(), func(l *jobv3.LanguageDTO, _ int) (string, bool) { return l.GetName(), l.GetName() != "" }),
		FullAddress:      strings.TrimSpace(strings.Join(lo.Compact([]string{d.GetFloorUnit(), d.GetAddress()}), ", ")),
		Description:      d.GetJobDescription(),
		Requirements:     d.GetJobRequirements(),
		RemoteCountries:  names(d.GetRemoteCountries()),
		ExternalApplyUrl: d.GetExternalApplyUrl(),
	}
}

func names(items []*jobv3.NamedItemDTO) []string {
	return lo.FilterMap(items, func(it *jobv3.NamedItemDTO, _ int) (string, bool) {
		n := mcpkit.FirstNonEmpty(it.GetName(), it.GetValue())
		return n, n != ""
	})
}

func jobType(id int64) openv1.JobType {
	if _, ok := openv1.JobType_name[int32(id)]; ok {
		return openv1.JobType(id)
	}
	return openv1.JobType_JOB_TYPE_UNSPECIFIED
}

func workArrangement(id int64) openv1.WorkArrangement {
	if _, ok := openv1.WorkArrangement_name[int32(id)]; ok {
		return openv1.WorkArrangement(id)
	}
	return openv1.WorkArrangement_WORK_ARRANGEMENT_UNSPECIFIED
}

// ============================================================================
// 面试
// ============================================================================

func interview(d *jobappv3.InterviewItem) *openv1.Interview {
	if d == nil {
		return nil
	}
	isVideo := d.GetVideoCategory() != "" || d.GetVideoLink() != "" || d.GetGoogleMeetMeetingId() > 0
	jobTitle := mcpkit.FirstNonEmpty(d.GetJobTitle(), d.GetJob().GetJobTitle())
	talentID := lo.Ternary(d.GetJobseekerId() > 0, d.GetJobseekerId(), d.GetJobseeker().GetId())
	out := &openv1.Interview{
		InterviewId:       d.GetId(),
		JobId:             d.GetJobId(),
		JobTitle:          jobTitle,
		TalentId:          talentID,
		TalentDisplayName: maskFullName(d.GetJobseeker().GetFullName()),
		InterviewTime:     d.GetInterviewedAt(),
		State:             interviewState(d.GetStatusId()),
		StatusText:        mcpkit.FirstNonEmpty(d.GetRecruiterDisplayStatus(), d.GetStatus()),
		IsVideo:           isVideo,
		ContactPerson:     d.GetContactPerson(),
		Result:            mcpkit.FirstNonEmpty(d.GetInterviewResult()),
		CancelledReason:   d.GetCancelledReason(),
	}
	if !isVideo {
		out.Address = strings.TrimSpace(strings.Join(lo.Compact([]string{d.GetFloorUnit(), d.GetAddress()}), ", "))
	}
	return out
}

func interviewState(s jobappv3.InterviewStatus) openv1.InterviewState {
	if _, ok := openv1.InterviewState_name[int32(s)]; ok {
		return openv1.InterviewState(s)
	}
	return openv1.InterviewState_INTERVIEW_STATE_UNSPECIFIED
}

// ============================================================================
// 招聘数据
// ============================================================================

func recruitmentStats(d *recruiterv3.DayStats) *openv1.RecruitmentStats {
	if d == nil {
		return nil
	}
	return &openv1.RecruitmentStats{
		ViewedTalents:       d.GetViewedTalent(),
		ViewedMe:            d.GetViewedMe(),
		GreetedTalents:      d.GetGreetedTalent(),
		GreetedMe:           d.GetGreetedMe(),
		Communicated:        d.GetCommunicated(),
		ResumesObtained:     d.GetResumeObtained(),
		ResumesReceived:     d.GetResumeReceived(),
		ContactsExchanged:   d.GetExchangedCall(),
		InterviewsInvited:   d.GetInterviewInvited(),
		InterviewsCompleted: d.GetInterviewCompleted(),
	}
}
