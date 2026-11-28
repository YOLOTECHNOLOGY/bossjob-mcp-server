package mcpkit

import (
	"context"
	"strconv"
	"strings"
	"time"

	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	openv1 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1"
	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SalaryInsight 求职者 / 招聘者共用的市场薪资洞察：职位名称与地点经 config-go 解析后调用 job-go SearchSalary。
// ctx 中的 CtxInfo.CountryID 决定站点与币种；req.country_code 非空时切换站点。
func SalaryInsight(ctx context.Context, search jobv3.JobSearchServiceClient, resolver Resolver, source string,
	req *openv1.GetSalaryInsightRequest) (*openv1.GetSalaryInsightResponse, error) {
	ctx, err := WithCountry(ctx, req.GetCountryCode())
	if err != nil {
		return nil, err
	}
	title, err := resolver.JobTitle(ctx, req.GetJobTitle())
	if err != nil {
		return nil, err
	}
	if title == nil || title.ID <= 0 {
		return nil, status.Errorf(codes.NotFound, "no standard job title matches %q; try a more common title", req.GetJobTitle())
	}
	pb := &jobv3.SearchSalaryRequest{FunctionJobTitleId: lo.ToPtr(title.ID), Source: source}
	matchedLocation := ""
	if req.GetLocation() != "" {
		loc, err := resolver.Location(ctx, req.GetLocation())
		if err != nil {
			return nil, err
		}
		if loc == nil {
			return nil, status.Errorf(codes.NotFound, "location %q is not a known city or region of this country site", req.GetLocation())
		}
		if loc.LocationID > 0 {
			pb.JobLocationId = lo.ToPtr(loc.LocationID)
		}
		if loc.RegionID > 0 {
			pb.JobRegionId = lo.ToPtr(loc.RegionID)
		}
		matchedLocation = loc.Name
	}
	resp, err := search.SearchSalary(ctx, pb)
	if err != nil {
		return nil, UpstreamError(ctx, "salary insight", err)
	}
	currency := ""
	if st, ok := SiteByID(int64(CurrentCountryID(ctx))); ok {
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
