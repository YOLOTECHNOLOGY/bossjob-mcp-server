package jobseeker

import (
	"strconv"
	"strings"

	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobseekerv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/jobseeker/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	"github.com/samber/lo"

	"bossjob-mcp-server/domain/mcpkit"
)

// converter 将领域服务 DTO 转为精简的 MCP 输出；不映射任何联系方式字段（如 RecruiterDTO.email）。
type converter struct {
	webBaseURL string
}

func (c converter) absURL(u string) string { return mcpkit.AbsURL(c.webBaseURL, u) }

func (c converter) company(d *jobv3.CompanyDTO) *openv1.CompanySummary {
	if d == nil || d.GetId() == 0 {
		return nil
	}
	return &openv1.CompanySummary{
		CompanyId:      d.GetId(),
		Name:           d.GetName(),
		LogoUrl:        lo.Ternary(d.GetLogoUrl() != "", d.GetLogoUrl(), d.GetLogo()),
		Industry:       d.GetIndustry(),
		CompanySize:    d.GetCompanySize(),
		FinancingStage: d.GetFinancingStage(),
		IsVerified:     d.GetIsVerify(),
		CompanyUrl:     c.absURL(d.GetCompanyUrl()),
	}
}

func salary(d *jobv3.JobDetailDTO) *openv1.Salary {
	period := openv1.SalaryPeriod(d.GetSalaryType().GetId())
	if _, ok := openv1.SalaryPeriod_name[int32(period)]; !ok {
		period = openv1.SalaryPeriod_SALARY_PERIOD_UNSPECIFIED
	}
	s := &openv1.Salary{
		CurrencyCode: d.GetCurrencyCode(),
		Period:       period,
		IsNegotiable: d.GetIsNegotiable(),
		IsHidden:     d.GetIsSalaryHidden(),
	}
	switch period {
	case openv1.SalaryPeriod_SALARY_PERIOD_DAILY:
		s.MinAmount, s.MaxAmount = d.GetDailySalaryRangeFrom(), d.GetDailySalaryRangeTo()
		s.DisplayText = mcpkit.FirstNonEmpty(d.GetLocalDailySalaryRangeValue(), d.GetDailySalaryRangeValue())
	case openv1.SalaryPeriod_SALARY_PERIOD_HOURLY:
		s.MinAmount, s.MaxAmount = d.GetHourlySalaryRangeFrom(), d.GetHourlySalaryRangeTo()
		s.DisplayText = mcpkit.FirstNonEmpty(d.GetLocalHourlySalaryRangeValue(), d.GetHourlySalaryRangeValue())
	case openv1.SalaryPeriod_SALARY_PERIOD_ANNUALLY:
		s.MinAmount, s.MaxAmount = d.GetAnnuallySalaryRangeFrom(), d.GetAnnuallySalaryRangeTo()
		s.DisplayText = mcpkit.FirstNonEmpty(d.GetLocalAnnuallySalaryRangeValue(), d.GetAnnuallySalaryRangeValue())
	default:
		s.MinAmount, s.MaxAmount = d.GetSalaryRangeFrom(), d.GetSalaryRangeTo()
		s.DisplayText = mcpkit.FirstNonEmpty(d.GetLocalSalaryRangeValue(), d.GetSalaryRangeValue())
	}
	if s.IsHidden {
		s.MinAmount, s.MaxAmount = "", ""
	}
	return s
}

func jobType(id int32) openv1.JobType {
	if _, ok := openv1.JobType_name[id]; ok {
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

func names(items []*jobv3.NamedItemDTO) []string {
	return lo.FilterMap(items, func(it *jobv3.NamedItemDTO, _ int) (string, bool) {
		n := mcpkit.FirstNonEmpty(it.GetName(), it.GetValue())
		return n, n != ""
	})
}

func (c converter) jobSummary(d *jobv3.JobDetailDTO) *openv1.JobSummary {
	if d == nil {
		return nil
	}
	return &openv1.JobSummary{
		JobId:           d.GetId(),
		Title:           d.GetJobTitle(),
		Company:         c.company(d.GetCompany()),
		Location:        mcpkit.FirstNonEmpty(d.GetJobLocation(), d.GetLocation().GetValue()),
		CountryCode:     mcpkit.CountryCode(d.GetJobCountryId()),
		Salary:          salary(d),
		JobType:         jobType(d.GetJobTypeId()),
		WorkArrangement: workArrangement(d.GetWorkArrangement().GetId()),
		ExperienceLevel: d.GetXpLvl().GetValue(),
		Degree:          d.GetDegree().GetValue(),
		Skills:          names(d.GetSkills()),
		Tags:            d.GetCardTags(),
		IsUrgent:        d.GetIsUrgent(),
		IsSaved:         d.GetIsSaved(),
		IsApplied:       d.GetIsApplied(),
		PublishedAt:     d.GetPublishedAt(),
		RefreshedAt:     d.GetRefreshedAt(),
		JobUrl:          c.absURL(d.GetJobUrl()),
	}
}

func (c converter) job(d *jobv3.JobDetailDTO) *openv1.Job {
	if d == nil {
		return nil
	}
	var recruiter *openv1.RecruiterSummary
	if r := d.GetRecruiter(); r != nil && r.GetId() != 0 {
		recruiter = &openv1.RecruiterSummary{
			DisplayName:  r.GetFullName(),
			JobTitle:     r.GetWorkExperience().GetJobTitle(),
			LastActiveAt: r.GetLastActiveAt(),
			ResponseRate: r.GetResponseRate(),
			ReplyLabel:   mcpkit.FirstNonEmpty(r.GetReplyLabel(), d.GetRecruiterReplyLabel()),
		}
	}
	return &openv1.Job{
		JobId:            d.GetId(),
		Title:            d.GetJobTitle(),
		Company:          c.company(d.GetCompany()),
		Recruiter:        recruiter,
		Location:         mcpkit.FirstNonEmpty(d.GetJobLocation(), d.GetLocation().GetValue()),
		CountryCode:      mcpkit.CountryCode(d.GetJobCountryId()),
		FullAddress:      d.GetFullAddress(),
		Salary:           salary(d),
		JobType:          jobType(d.GetJobTypeId()),
		WorkArrangement:  workArrangement(d.GetWorkArrangement().GetId()),
		ExperienceLevel:  d.GetXpLvl().GetValue(),
		Degree:           d.GetDegree().GetValue(),
		JobFunction:      mcpkit.FirstNonEmpty(d.GetFunctionJobTitle(), d.GetFunction().GetSubFunction(), d.GetFunction().GetMainFunction()),
		Skills:           names(d.GetSkills()),
		Benefits:         append(names(d.GetBenefits()), names(d.GetRemoteBenefits())...),
		Languages:        lo.Map(d.GetLanguages(), func(l *jobv3.LanguageDTO, _ int) string { return l.GetName() }),
		Description:      d.GetJobDescription(),
		Requirements:     d.GetJobRequirements(),
		IsUrgent:         d.GetIsUrgent(),
		IsSaved:          d.GetIsSaved(),
		IsApplied:        d.GetIsApplied(),
		IsResumeRequired: d.GetIsResumeRequired(),
		RemoteCountries:  names(d.GetRemoteCountries()),
		ExternalApplyUrl: d.GetExternalApplyUrl(),
		PublishedAt:      d.GetPublishedAt(),
		RefreshedAt:      d.GetRefreshedAt(),
		ExpireTime:       d.GetExpiredAt(),
		JobUrl:           c.absURL(d.GetJobUrl()),
	}
}

// enumText 将 JOBSEEKING_STATUS_ACTIVELY_LOOKING 之类的枚举名转为 "Actively looking"。
func enumText(name, prefix string) string {
	name = strings.TrimPrefix(name, prefix)
	if name == "" || name == "UNSPECIFIED" {
		return ""
	}
	text := strings.ToLower(strings.ReplaceAll(name, "_", " "))
	return strings.ToUpper(text[:1]) + text[1:]
}

func monthlySalary(from, to int64, currency string) *openv1.Salary {
	if from == 0 && to == 0 {
		return nil
	}
	s := &openv1.Salary{CurrencyCode: currency, Period: openv1.SalaryPeriod_SALARY_PERIOD_MONTHLY}
	if from > 0 {
		s.MinAmount = formatInt(from)
	}
	if to > 0 {
		s.MaxAmount = formatInt(to)
	}
	return s
}

func profile(p *jobseekerv3.JobseekerProfile) *openv1.Profile {
	if p == nil {
		return nil
	}
	return &openv1.Profile{
		FullName:          mcpkit.FirstNonEmpty(p.GetFullName(), strings.TrimSpace(p.GetFirstName()+" "+p.GetLastName())),
		Location:          p.GetLocation(),
		Nationality:       p.GetNationality(),
		Summary:           mcpkit.FirstNonEmpty(p.GetSummary(), p.GetDescription()),
		YearsOfExperience: p.GetYearsOfExperience(),
		HighestDegree:     p.GetDegreeName(),
		NoticePeriod:      mcpkit.FirstNonEmpty(p.GetNoticePeriod(), enumText(p.GetNoticePeriodType().String(), "NOTICE_PERIOD_")),
		JobseekingStatus:  enumText(p.GetJobseekingStatus().String(), "JOBSEEKING_STATUS_"),
		ExpectedSalary:    monthlySalary(p.GetExpectedSalaryFrom(), p.GetExpectedSalaryTo(), p.GetExpectedSalaryCurrency()),
		WorkExperiences: lo.Map(p.GetWorkExperiences(), func(w *jobseekerv3.WorkExperience, _ int) *openv1.WorkExperience {
			return &openv1.WorkExperience{
				JobTitle: w.GetJobTitle(), CompanyName: w.GetCompanyName(), Industry: w.GetIndustryName(),
				Location: w.GetLocationName(), StartDate: yearMonth(w.GetStartDate()), EndDate: yearMonth(w.GetEndDate()),
				IsCurrentJob: w.GetIsCurrentJob(), Description: w.GetDescription(), Skills: w.GetSkills(),
			}
		}),
		Educations: lo.Map(p.GetEducations(), func(e *jobseekerv3.Education, _ int) *openv1.Education {
			return &openv1.Education{
				School: e.GetSchool(), Degree: e.GetDegree(), FieldOfStudy: e.GetFieldStudy(),
				StartDate: yearMonth(e.GetStartDate()), EndDate: yearMonth(e.GetEndDate()), IsCurrentStudy: e.GetIsCurrentStudy(),
			}
		}),
		Skills: lo.Map(p.GetSkills(), func(s *jobseekerv3.Skill, _ int) string { return s.GetName() }),
		Languages: lo.Map(p.GetLanguages(), func(l *jobseekerv3.Language, _ int) string {
			if level := enumText(l.GetProficiency().String(), "LANGUAGE_PROFICIENCY_"); level != "" {
				return l.GetLanguageName() + " (" + level + ")"
			}
			return l.GetLanguageName()
		}),
		Certifications: lo.Map(p.GetLicenseCertifications(), func(c *jobseekerv3.LicenseCertification, _ int) string { return c.GetName() }),
		HasResume:      len(p.GetResumes()) > 0,
	}
}

func jobPreference(p *jobseekerv3.JobPreference) *openv1.JobPreference {
	out := &openv1.JobPreference{
		JobPreferenceId: p.GetId(),
		JobTitle:        p.GetJobTitle(),
		Locations:       p.GetLocationNames(),
		Industries:      p.GetIndustryNames(),
		JobFunction:     p.GetFunctionName(),
		ExpectedSalary:  monthlySalary(p.GetSalaryFrom(), p.GetSalaryTo(), p.GetSalaryCurrency()),
		JobTypes: lo.FilterMap(p.GetJobTypes(), func(t string, _ int) (openv1.JobType, bool) {
			v := jobTypeByName(t)
			return v, v != openv1.JobType_JOB_TYPE_UNSPECIFIED
		}),
	}
	if wa := openv1.WorkArrangement(p.GetWorkArrangement()); wa != openv1.WorkArrangement_WORK_ARRANGEMENT_UNSPECIFIED {
		if _, ok := openv1.WorkArrangement_name[int32(wa)]; ok {
			out.WorkArrangements = []openv1.WorkArrangement{wa}
		}
	}
	return out
}

// jobTypeByName 将偏好中的职位类型文本（如 "Full-time"、"full_time"）映射为枚举。
func jobTypeByName(s string) openv1.JobType {
	key := "JOB_TYPE_" + strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(s)))
	if v, ok := openv1.JobType_value[key]; ok {
		return openv1.JobType(v)
	}
	return openv1.JobType_JOB_TYPE_UNSPECIFIED
}

// yearMonth 将 "2023-05-01" / "2023-05" 等日期截为 "YYYY-MM"。
func yearMonth(s string) string {
	if len(s) >= 7 && s[4] == '-' {
		return s[:7]
	}
	return s
}

func formatInt(v int64) string { return strconv.FormatInt(v, 10) }
