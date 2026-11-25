package oauthrs

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// Middleware 对 MCP 端点执行 Bearer Token 校验（RFC 6750 / RFC 9728）。
type Middleware struct {
	verifier            Verifier
	resourceMetadataURL string
}

// NewMiddleware 创建鉴权中间件；resourceMetadataURL 写入 WWW-Authenticate 供客户端发现授权服务器。
func NewMiddleware(verifier Verifier, resourceMetadataURL string) *Middleware {
	return &Middleware{verifier: verifier, resourceMetadataURL: resourceMetadataURL}
}

// ResourceMetadataURL 根据 resource 与 RFC 9728 路径生成元数据绝对地址。
func ResourceMetadataURL(resource, metadataPath string) string {
	u, err := url.Parse(resource)
	if err != nil || u.Host == "" {
		return metadataPath
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: metadataPath}).String()
}

// Wrap 校验通过后把身份与 CtxInfo 写入请求 context。
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		raw, present := bearerToken(r)
		if !present {
			m.reject(w, http.StatusUnauthorized, "", "")
			return
		}
		p, err := m.verifier.Verify(r.Context(), raw)
		switch {
		case errors.Is(err, ErrInsufficientScope):
			m.reject(w, http.StatusForbidden, "insufficient_scope", "The access token lacks the required scope")
			return
		case err != nil:
			m.reject(w, http.StatusUnauthorized, "invalid_token", "The access token is invalid or expired")
			return
		}
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.NewString()
		}
		w.Header().Set("X-Trace-ID", traceID)
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p, traceID)))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func (m *Middleware) reject(w http.ResponseWriter, status int, code, desc string) {
	challenge := `Bearer resource_metadata="` + m.resourceMetadataURL + `"`
	if code != "" {
		challenge += `, error="` + code + `", error_description="` + desc + `"`
	}
	if code == "insufficient_scope" {
		challenge += `, scope="` + AccessScope + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if code == "" {
		code = "invalid_request"
		desc = "Missing bearer token"
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}
