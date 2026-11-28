package mcpkit

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	DefaultPageSize = 10
	MaxPageSize     = 20
	pageTokenPrefix = "p1:"
)

func PageSize(n int32) int32 {
	if n <= 0 {
		return DefaultPageSize
	}
	if n > MaxPageSize {
		return MaxPageSize
	}
	return n
}

// DecodePage 解析分页 token（不透明 base64url，内容为页码），空 token 表示第 1 页。
func DecodePage(token string) (int32, error) {
	if token == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(raw), pageTokenPrefix) {
		return 0, InvalidArgument("page_token is invalid; pass next_page_token from the previous response")
	}
	page, err := strconv.Atoi(strings.TrimPrefix(string(raw), pageTokenPrefix))
	if err != nil || page < 1 || page > 1000 {
		return 0, InvalidArgument("page_token is invalid; pass next_page_token from the previous response")
	}
	return int32(page), nil
}

func EncodePage(page int32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(pageTokenPrefix + strconv.Itoa(int(page))))
}

// NextPageToken 在还有下一页时返回 token。
func NextPageToken(page, totalPages int32) string {
	if totalPages > page {
		return EncodePage(page + 1)
	}
	return ""
}
