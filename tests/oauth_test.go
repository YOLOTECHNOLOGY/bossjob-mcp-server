package tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bossjob-mcp-server/domain/oauthrs"
	"bossjob-mcp-server/pub/digutil"

	"github.com/bytedance/mockey"
	"github.com/golang-jwt/jwt/v5"
	"github.com/smartystreets/goconvey/convey"
)

func TestCI_OAuthVerifier_Verify(t *testing.T) {
	mockey.PatchConvey("Feature: MCP 只接受本环境 auth-go 签发的求职者 Access Token", t, func() {
		e := newEnv(t)
		v := digutil.MustGet[oauthrs.Verifiers](e.c).Jobseeker
		ctx := context.Background()

		mockey.PatchConvey("Scenario: 合法 Token 解析出 uid / country_id 并组装 CtxInfo", func() {
			p, err := v.Verify(ctx, signToken(t, jwt.MapClaims{"country_id": 193}))
			convey.So(err, convey.ShouldBeNil)
			convey.So(p.UserID, convey.ShouldEqual, testUserID)
			convey.So(p.CountryID, convey.ShouldEqual, 193)
			convey.So(p.Subject, convey.ShouldEqual, "opaque-subject")
			ci := p.CtxInfo("trace-1")
			convey.So(ci.UserID, convey.ShouldEqual, testUserID)
			convey.So(ci.RoleID, convey.ShouldEqual, 1)
			convey.So(ci.LanguageID, convey.ShouldEqual, 1)
			convey.So(ci.CountryID, convey.ShouldEqual, 193)
			convey.So(ci.SID, convey.ShouldEqual, 0)
			convey.So(ci.TraceID, convey.ShouldEqual, "trace-1")
		})
		mockey.PatchConvey("Scenario: 缺少 country_id 时回退 PH(167)", func() {
			p, err := v.Verify(ctx, signToken(t, jwt.MapClaims{"country_id": nil}))
			convey.So(err, convey.ShouldBeNil)
			convey.So(p.CountryID, convey.ShouldEqual, 167)
		})
		mockey.PatchConvey("Scenario: 声明不符合本资源的 Token 一律拒绝", func() {
			cases := map[string]jwt.MapClaims{
				"其他环境 issuer":   {"iss": "https://auth.bossjob.com"},
				"招聘者 resource":  {"aud": "https://dev.mcp.bossjob.com/recruiter/mcp"},
				"招聘者角色":         {"role": "recruiter"},
				"已过期":           {"exp": time.Now().Add(-time.Hour).Unix(), "iat": time.Now().Add(-2 * time.Hour).Unix()},
				"缺少 exp":        {"exp": nil},
				"缺少 uid":        {"uid": nil},
				"uid 非正整数":      {"uid": 1.5},
				"缺少 sub":        {"sub": nil},
				"country_id 非法": {"country_id": "PH"},
				"未知 kid":        {"kid": "other-kid"},
			}
			for name, overrides := range cases {
				_, err := v.Verify(ctx, signToken(t, overrides))
				convey.So(err, convey.ShouldEqual, oauthrs.ErrInvalidToken)
				_ = name
			}
		})
		mockey.PatchConvey("Scenario: 缺少 mcp:access scope 返回 insufficient_scope", func() {
			_, err := v.Verify(ctx, signToken(t, jwt.MapClaims{"scope": "openid"}))
			convey.So(err, convey.ShouldEqual, oauthrs.ErrInsufficientScope)
		})
		mockey.PatchConvey("Scenario: 非 RS256 签名（HS256 / none）被拒绝", func() {
			hs := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": testIssuer, "aud": testResource, "role": "jobseeker", "uid": testUserID, "sub": "x", "scope": "mcp:access", "exp": time.Now().Add(time.Minute).Unix()})
			hs.Header["kid"] = testKid
			raw, _ := hs.SignedString([]byte("secret"))
			_, err := v.Verify(ctx, raw)
			convey.So(err, convey.ShouldEqual, oauthrs.ErrInvalidToken)
			_, err = v.Verify(ctx, "")
			convey.So(err, convey.ShouldEqual, oauthrs.ErrInvalidToken)
		})
	})
}

func TestCI_JWKSKeySource_Key(t *testing.T) {
	mockey.PatchConvey("Feature: 从 auth-go JWKS 拉取并缓存验签公钥", t, func() {
		pub := signingKey(t).PublicKey
		var fetches atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fetches.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": testKid,
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}}})
		}))
		defer srv.Close()
		ks := oauthrs.NewJWKSKeySource(srv.URL, time.Hour, srv.Client())

		mockey.PatchConvey("Scenario: 已知 kid 首次拉取后命中缓存", func() {
			k, err := ks.Key(context.Background(), testKid)
			convey.So(err, convey.ShouldBeNil)
			convey.So(k.N.Cmp(pub.N), convey.ShouldEqual, 0)
			_, err = ks.Key(context.Background(), testKid)
			convey.So(err, convey.ShouldBeNil)
			convey.So(fetches.Load(), convey.ShouldEqual, 1)
		})
		mockey.PatchConvey("Scenario: 未知 kid 在最短刷新间隔内不重复拉取", func() {
			_, err := ks.Key(context.Background(), testKid)
			convey.So(err, convey.ShouldBeNil)
			_, err = ks.Key(context.Background(), "rotated-kid")
			convey.So(err, convey.ShouldEqual, oauthrs.ErrUnknownKey)
			convey.So(fetches.Load(), convey.ShouldEqual, 1)
		})
	})
}

func TestCI_MCPApp_HTTPAuth(t *testing.T) {
	mockey.PatchConvey("Feature: MCP HTTP 入口执行 Bearer 校验并公布 RFC 9728 元数据", t, func() {
		e := newEnv(t)
		srv := e.server(t)
		post := func(token string) *http.Response {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/jobseeker/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := http.DefaultClient.Do(req)
			convey.So(err, convey.ShouldBeNil)
			return resp
		}
		metadataURL := "https://dev.mcp.bossjob.com/.well-known/oauth-protected-resource/jobseeker/mcp"

		mockey.PatchConvey("Scenario: 健康检查无需 Token", func() {
			resp, err := http.Get(srv.URL + "/health")
			convey.So(err, convey.ShouldBeNil)
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusOK)
		})
		mockey.PatchConvey("Scenario: 受保护资源元数据指向本环境 issuer", func() {
			resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/jobseeker/mcp")
			convey.So(err, convey.ShouldBeNil)
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusOK)
			convey.So(body["resource"], convey.ShouldEqual, testResource)
			convey.So(body["authorization_servers"], convey.ShouldResemble, []any{testIssuer})
			convey.So(body["scopes_supported"], convey.ShouldResemble, []any{"mcp:access"})
		})
		mockey.PatchConvey("Scenario: 无 Token 返回 401 与 resource_metadata 挑战", func() {
			resp := post("")
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusUnauthorized)
			convey.So(resp.Header.Get("WWW-Authenticate"), convey.ShouldEqual, `Bearer resource_metadata="`+metadataURL+`"`)
		})
		mockey.PatchConvey("Scenario: 非法 Token 返回 401 invalid_token", func() {
			resp := post(signToken(t, jwt.MapClaims{"aud": "https://other/mcp"}))
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusUnauthorized)
			convey.So(resp.Header.Get("WWW-Authenticate"), convey.ShouldContainSubstring, `error="invalid_token"`)
			body, _ := io.ReadAll(resp.Body)
			convey.So(string(body), convey.ShouldNotContainSubstring, "aud")
		})
		mockey.PatchConvey("Scenario: 缺少 scope 返回 403 insufficient_scope", func() {
			resp := post(signToken(t, jwt.MapClaims{"scope": "other"}))
			convey.So(resp.StatusCode, convey.ShouldEqual, http.StatusForbidden)
			convey.So(resp.Header.Get("WWW-Authenticate"), convey.ShouldContainSubstring, `error="insufficient_scope"`)
		})
		mockey.PatchConvey("Scenario: 合法 Token 可完成 initialize 并列出 10 个工具", func() {
			cli := e.client(t, signToken(t, nil))
			tools, err := cli.ListTools(context.Background(), mcpListToolsRequest())
			convey.So(err, convey.ShouldBeNil)
			names := make([]string, 0, len(tools.Tools))
			for _, tool := range tools.Tools {
				names = append(names, tool.Name)
			}
			convey.So(names, convey.ShouldHaveLength, 10)
			for _, n := range []string{"search_jobs", "get_job", "list_recommended_jobs", "get_my_profile", "evaluate_job_match",
				"list_my_applications", "list_saved_jobs", "save_job", "unsave_job", "get_salary_insight"} {
				convey.So(names, convey.ShouldContain, n)
			}
		})
	})
}
