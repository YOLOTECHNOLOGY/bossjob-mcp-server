package jobseeker

import (
	"context"
	"strings"

	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/ctxutil"
)

// site 为 Bossjob 国家站点：country_id 与 api-go CountryIdKeyMap 一致，currency 为 ISO 4217。
type site struct {
	ID       int32
	Code     string
	Currency string
}

var sites = []site{
	{92, "HK", "HKD"}, {96, "ID", "IDR"}, {105, "JP", "JPY"}, {123, "MO", "MOP"},
	{127, "MY", "MYR"}, {167, "PH", "PHP"}, {188, "SA", "SAR"}, {193, "SG", "SGD"},
	{211, "TW", "TWD"}, {220, "TR", "TRY"}, {38, "CA", "CAD"}, {228, "GB", "GBP"},
	{229, "US", "USD"}, {154, "NG", "NGN"}, {227, "AE", "AED"}, {203, "LK", "LKR"},
	{202, "ES", "EUR"}, {190, "RS", "RSD"}, {29, "BR", "BRL"},
}

func siteByCode(code string) (site, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, s := range sites {
		if s.Code == code {
			return s, true
		}
	}
	return site{}, false
}

func siteByID(id int64) (site, bool) {
	for _, s := range sites {
		if int64(s.ID) == id {
			return s, true
		}
	}
	return site{}, false
}

// withCountry 返回切换到指定国家站点的 context（复制 CtxInfo，不修改调用方的身份）。
func withCountry(ctx context.Context, countryCode string) (context.Context, error) {
	if countryCode == "" {
		return ctx, nil
	}
	s, ok := siteByCode(countryCode)
	if !ok {
		return ctx, invalidArgument("country_code %q is not a supported Bossjob country site", countryCode)
	}
	cinfo := ctxutil.GetCtxInfo(ctx)
	if cinfo == nil {
		return ctx, nil
	}
	copied := *cinfo
	copied.CountryID = s.ID
	return ctxutil.WithCtxInfo(ctx, &copied), nil
}

func currentCountryID(ctx context.Context) int32 {
	if cinfo := ctxutil.GetCtxInfo(ctx); cinfo != nil {
		return cinfo.CountryID
	}
	return 0
}
