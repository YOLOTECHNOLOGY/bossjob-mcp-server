package mcpkit

import (
	"context"
	"strings"

	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/ctxutil"
)

// site 为 Bossjob 国家站点，与 jobseeker-web helpers/country.base.js countryConfig 及 auth-go 登录页选项一致。
// Code 为 ISO 3166-1 alpha-2（工具参数 country_code），Key 为 config-go country_key，Currency 为 ISO 4217。
type Site struct {
	ID       int32
	Code     string
	Key      string
	Currency string
}

var Sites = []Site{
	{105, "JP", "jp", "JPY"}, {167, "PH", "ph", "PHP"}, {193, "SG", "sg", "SGD"}, {127, "MY", "my", "MYR"},
	{96, "ID", "id", "IDR"}, {92, "HK", "hk", "HKD"}, {211, "TW", "tw", "TWD"}, {229, "US", "us", "USD"},
	{228, "GB", "gb", "GBP"}, {38, "CA", "ca", "CAD"}, {29, "BR", "br", "BRL"}, {220, "TR", "tr", "TRY"},
	{154, "NG", "ng", "NGN"}, {188, "SA", "sa", "SAR"}, {227, "AE", "ae", "AED"}, {203, "LK", "lk", "LKR"},
	{202, "ES", "es", "EUR"}, {190, "RS", "rs", "RSD"},
	// Others：全球站，无 ISO 国家代码与本币，不能通过 country_code 选择，只能来自 Token country_id。
	{241, "", "com", ""},
}

func SiteByCode(code string) (Site, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, s := range Sites {
		if code != "" && s.Code == code {
			return s, true
		}
	}
	return Site{}, false
}

func SiteByID(id int64) (Site, bool) {
	for _, s := range Sites {
		if int64(s.ID) == id {
			return s, true
		}
	}
	return Site{}, false
}

// WithCountry 返回切换到指定国家站点的 context（复制 CtxInfo，不修改调用方的身份）。
func WithCountry(ctx context.Context, countryCode string) (context.Context, error) {
	if countryCode == "" {
		return ctx, nil
	}
	s, ok := SiteByCode(countryCode)
	if !ok {
		return ctx, InvalidArgument("country_code %q is not a supported Bossjob country site", countryCode)
	}
	cinfo := ctxutil.GetCtxInfo(ctx)
	if cinfo == nil {
		return ctx, nil
	}
	copied := *cinfo
	copied.CountryID = s.ID
	return ctxutil.WithCtxInfo(ctx, &copied), nil
}

func CurrentCountryID(ctx context.Context) int32 {
	if cinfo := ctxutil.GetCtxInfo(ctx); cinfo != nil {
		return cinfo.CountryID
	}
	return 0
}
