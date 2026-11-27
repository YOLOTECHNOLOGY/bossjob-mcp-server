package mcpkit

import (
	"strconv"
	"strings"

	"github.com/samber/lo"
)

// FirstNonEmpty 返回第一个非空字符串。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// JoinIDs 去重后以逗号拼接 ID（下游 CSV 参数）。
func JoinIDs(ids []int64) string {
	return strings.Join(lo.Map(lo.Uniq(ids), func(v int64, _ int) string { return strconv.FormatInt(v, 10) }), ",")
}

// JoinEnums 去重后以逗号拼接枚举数值。
func JoinEnums[T ~int32](values []T) string {
	return strings.Join(lo.Map(lo.Uniq(values), func(v T, _ int) string { return strconv.Itoa(int(v)) }), ",")
}

// AbsURL 将站内相对路径拼接为站点绝对地址。
func AbsURL(webBaseURL, u string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || webBaseURL == "" {
		return u
	}
	return strings.TrimRight(webBaseURL, "/") + "/" + strings.TrimLeft(u, "/")
}

// CountryCode 站点 ID 转 ISO 国家代码；未知站点返回空。
func CountryCode(id int64) string {
	if s, ok := SiteByID(id); ok {
		return s.Code
	}
	return ""
}
