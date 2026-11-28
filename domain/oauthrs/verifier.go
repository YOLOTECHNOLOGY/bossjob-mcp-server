package oauthrs

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrInvalidToken Token 缺失、验签失败、过期或声明不符合本资源。
	ErrInvalidToken = errors.New("invalid_token")
	// ErrInsufficientScope Token 有效但缺少所需 scope。
	ErrInsufficientScope = errors.New("insufficient_scope")
)

// Verifier 校验 Access Token 并返回身份。
type Verifier interface {
	Verify(ctx context.Context, raw string) (*Principal, error)
}

// Verifiers 每个 MCP resource 各自一个校验器：求职者端点只接受 role=jobseeker + 求职者 resource 的 Token，招聘者同理。
type Verifiers struct {
	Jobseeker Verifier
	Recruiter Verifier
}

// VerifierConfig 单个 MCP resource 的校验规则。
type VerifierConfig struct {
	Issuer    string
	Resource  string
	Role      string
	ClockSkew time.Duration
}

type jwtVerifier struct {
	cfg  VerifierConfig
	keys KeySource
}

// NewVerifier 创建 RS256 JWT 校验器：校验 kid、iss、aud、exp、scope、role 与 uid。
func NewVerifier(cfg VerifierConfig, keys KeySource) Verifier {
	return &jwtVerifier{cfg: cfg, keys: keys}
}

func (v *jwtVerifier) Verify(ctx context.Context, raw string) (*Principal, error) {
	if raw == "" {
		return nil, ErrInvalidToken
	}
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, ErrInvalidToken
		}
		return v.keys.Key(ctx, kid)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Resource),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(v.cfg.ClockSkew),
	)
	if err != nil {
		return nil, ErrInvalidToken
	}

	role, _ := claims["role"].(string)
	if role != v.cfg.Role {
		return nil, ErrInvalidToken
	}
	sub, _ := claims["sub"].(string)
	uid, ok := positiveInt(claims["uid"], math.MaxInt64)
	if sub == "" || !ok {
		return nil, ErrInvalidToken
	}
	countryID := DefaultCountryID
	if raw, present := claims["country_id"]; present {
		c, ok := positiveInt(raw, math.MaxInt32)
		if !ok {
			return nil, ErrInvalidToken
		}
		countryID = int32(c)
	}
	scopeText, _ := claims["scope"].(string)
	scopes := strings.Fields(scopeText)
	if !contains(scopes, AccessScope) {
		return nil, ErrInsufficientScope
	}
	p := &Principal{Subject: sub, UserID: uid, CountryID: countryID, Role: role, Scopes: scopes}
	p.TokenID, _ = claims["jti"].(string)
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		p.ExpiresAt = exp.Time
	}
	return p, nil
}

// positiveInt 解析 JSON 数字声明为正整数（拒绝小数与越界值）。
func positiveInt(v any, max int64) (int64, bool) {
	f, ok := v.(float64)
	if !ok || f < 1 || f > float64(max) || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
