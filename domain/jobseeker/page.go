package jobseeker

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 10
	maxPageSize     = 20
	pageTokenPrefix = "p1:"
)

func pageSize(n int32) int32 {
	if n <= 0 {
		return defaultPageSize
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}

// decodePage 解析分页 token（不透明 base64url，内容为页码），空 token 表示第 1 页。
func decodePage(token string) (int32, error) {
	if token == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(raw), pageTokenPrefix) {
		return 0, invalidArgument("page_token is invalid; pass next_page_token from the previous response")
	}
	page, err := strconv.Atoi(strings.TrimPrefix(string(raw), pageTokenPrefix))
	if err != nil || page < 1 || page > 1000 {
		return 0, invalidArgument("page_token is invalid; pass next_page_token from the previous response")
	}
	return int32(page), nil
}

func encodePage(page int32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(pageTokenPrefix + strconv.Itoa(int(page))))
}

// nextPageToken 在还有下一页时返回 token。
func nextPageToken(page, totalPages int32) string {
	if totalPages > page {
		return encodePage(page + 1)
	}
	return ""
}
