package recruiter

import (
	"encoding/base64"
	"strconv"
	"strings"

	"bossjob-mcp-server/domain/mcpkit"
)

// 人才搜索 / 推荐的分页 token 额外携带下游 query_id，保证翻页时结果集一致。
const queryPageTokenPrefix = "q1:"

func decodeQueryPage(token string) (int32, string, error) {
	if token == "" {
		return 1, "", nil
	}
	invalid := mcpkit.InvalidArgument("page_token is invalid; pass next_page_token from the previous response")
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(raw), queryPageTokenPrefix) {
		return 0, "", invalid
	}
	pageStr, queryID, _ := strings.Cut(strings.TrimPrefix(string(raw), queryPageTokenPrefix), ":")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 || page > 1000 {
		return 0, "", invalid
	}
	return int32(page), queryID, nil
}

func nextQueryPageToken(page, totalPages int32, queryID string) string {
	if totalPages <= page {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(queryPageTokenPrefix + strconv.Itoa(int(page+1)) + ":" + queryID))
}
