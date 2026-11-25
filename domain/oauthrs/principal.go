// Package oauthrs 实现 MCP 作为 OAuth 资源服务器的 Access Token 校验与调用身份组装。
package oauthrs

import (
	"context"
	"time"

	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/ctxutil"
)

const (
	// RoleJobseeker auth-go Token 中求职者角色。
	RoleJobseeker = "jobseeker"
	// RoleRecruiter auth-go Token 中招聘者角色。
	RoleRecruiter = "recruiter"
	// AccessScope MCP 访问所需 scope。
	AccessScope = "mcp:access"

	// DefaultCountryID Token 缺少 country_id 时的默认站点（PH），与 auth-go / job-go 默认值一致。
	DefaultCountryID int32 = 167
	// DefaultLanguageID 下游调用固定使用英语。
	DefaultLanguageID int32 = 1
	// DefaultSiteID 下游调用固定站点 ID（与 lib-go header 中间件默认值一致）。
	DefaultSiteID int32 = 1
	// DeviceType 标识请求来自 MCP。
	DeviceType = "mcp"
)

// Principal 为通过校验的 Access Token 身份。
type Principal struct {
	Subject   string
	UserID    int64
	CountryID int32
	Role      string
	Scopes    []string
	TokenID   string
	ExpiresAt time.Time
}

// RoleID 将 Token role 转为 ctxutil RoleID（1 求职者 / 2 招聘者）。
func (p *Principal) RoleID() int8 {
	switch p.Role {
	case RoleJobseeker:
		return 1
	case RoleRecruiter:
		return 2
	}
	return 0
}

// CtxInfo 组装下游 gRPC 调用的 CtxInfo：RoleID 由 role 解析、LanguageID 固定英语、SID 不传。
func (p *Principal) CtxInfo(traceID string) *ctxutil.CtxInfo {
	return &ctxutil.CtxInfo{
		UserInfo:   ctxutil.UserInfo{UserID: p.UserID, RoleID: p.RoleID()},
		CountryID:  p.CountryID,
		LanguageID: DefaultLanguageID,
		SiteID:     DefaultSiteID,
		DeviceType: DeviceType,
		TraceID:    traceID,
	}
}

type principalKey struct{}

// WithPrincipal 将身份写入 context，并同步写入 ctxutil.CtxInfo 供 gRPC 客户端拦截器透传。
func WithPrincipal(ctx context.Context, p *Principal, traceID string) context.Context {
	ctx = context.WithValue(ctx, principalKey{}, p)
	return ctxutil.WithCtxInfo(ctx, p.CtxInfo(traceID))
}

// PrincipalFrom 读取当前身份；未认证返回 nil。
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
