package tests

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"bossjob-mcp-server/app"
	"bossjob-mcp-server/di"
	"bossjob-mcp-server/domain/oauthrs"
	"bossjob-mcp-server/pub/digutil"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobseekerv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/jobseeker/v3"
	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/ctxutil"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/dig"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	testIssuer   = "https://dev.auth.bossjob.com"
	testResource = "https://dev.mcp.bossjob.com/jobseeker/mcp"
	testKid      = "test-kid"
	testUserID   = 1001
)

var (
	keyOnce sync.Once
	testKey *rsa.PrivateKey
)

func signingKey(t *testing.T) *rsa.PrivateKey {
	keyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		testKey = k
	})
	return testKey
}

type staticKeys map[string]*rsa.PublicKey

func (s staticKeys) Key(_ context.Context, kid string) (*rsa.PublicKey, error) {
	if k, ok := s[kid]; ok {
		return k, nil
	}
	return nil, oauthrs.ErrUnknownKey
}

// signToken 生成与 auth-go 一致的 Access Token；overrides 中值为 nil 的声明会被删除。
func signToken(t *testing.T, overrides jwt.MapClaims) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": testIssuer, "sub": "opaque-subject", "aud": testResource, "iat": now.Unix(), "nbf": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(), "jti": "jti-1", "role": "jobseeker", "scope": "mcp:access",
		"uid": testUserID, "country_id": 167,
	}
	kid := testKid
	for k, v := range overrides {
		if k == "kid" {
			kid = v.(string)
			continue
		}
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(signingKey(t))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// call 记录一次下游调用：请求与当时的 CtxInfo（即 climw.CtxMD 会透传给下游的身份）。
type call struct {
	req   proto.Message
	cinfo ctxutil.CtxInfo
}

type recorder struct {
	mu    sync.Mutex
	calls map[string][]call
	resp  map[string]any
	errs  map[string]error
}

func newRecorder() *recorder {
	return &recorder{calls: map[string][]call{}, resp: map[string]any{}, errs: map[string]error{}}
}

func (r *recorder) record(ctx context.Context, method string, req proto.Message) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := call{req: proto.Clone(req)}
	if ci := ctxutil.GetCtxInfo(ctx); ci != nil {
		c.cinfo = *ci
	}
	r.calls[method] = append(r.calls[method], c)
	return r.resp[method], r.errs[method]
}

func (r *recorder) last(method string) (call, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := r.calls[method]
	if len(cs) == 0 {
		return call{}, false
	}
	return cs[len(cs)-1], true
}

func (r *recorder) count(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls[method])
}

func (r *recorder) set(method string, resp any, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resp[method], r.errs[method] = resp, err
}

func typed[T any](v any, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	if v == nil {
		return zero, nil
	}
	return v.(T), nil
}

type fakeJobs struct {
	jobv3.JobServiceClient
	*recorder
}

func (f *fakeJobs) GetJobDetail(ctx context.Context, in *jobv3.GetJobDetailRequest, _ ...grpc.CallOption) (*jobv3.JobDetailDTO, error) {
	return typed[*jobv3.JobDetailDTO](f.record(ctx, "GetJobDetail", in))
}
func (f *fakeJobs) ListSavedJobs(ctx context.Context, in *jobv3.ListSavedJobsRequest, _ ...grpc.CallOption) (*jobv3.ListSavedJobsResponse, error) {
	return typed[*jobv3.ListSavedJobsResponse](f.record(ctx, "ListSavedJobs", in))
}
func (f *fakeJobs) CreateSavedJob(ctx context.Context, in *jobv3.CreateSavedJobRequest, _ ...grpc.CallOption) (*jobv3.CreateSavedJobResponse, error) {
	return typed[*jobv3.CreateSavedJobResponse](f.record(ctx, "CreateSavedJob", in))
}
func (f *fakeJobs) DeleteSavedJob(ctx context.Context, in *jobv3.DeleteSavedJobRequest, _ ...grpc.CallOption) (*jobv3.JobActionResponse, error) {
	return typed[*jobv3.JobActionResponse](f.record(ctx, "DeleteSavedJob", in))
}
func (f *fakeJobs) ListAppliedJobs(ctx context.Context, in *jobv3.ListAppliedJobsRequest, _ ...grpc.CallOption) (*jobv3.ListAppliedJobsResponse, error) {
	return typed[*jobv3.ListAppliedJobsResponse](f.record(ctx, "ListAppliedJobs", in))
}

type fakeJobSearch struct {
	jobv3.JobSearchServiceClient
	*recorder
}

func (f *fakeJobSearch) SearchJobs(ctx context.Context, in *jobv3.SearchJobsRequest, _ ...grpc.CallOption) (*jobv3.SearchJobsResponse, error) {
	return typed[*jobv3.SearchJobsResponse](f.record(ctx, "SearchJobs", in))
}
func (f *fakeJobSearch) SearchRemoteJobs(ctx context.Context, in *jobv3.SearchRemoteJobsRequest, _ ...grpc.CallOption) (*jobv3.SearchJobsResponse, error) {
	return typed[*jobv3.SearchJobsResponse](f.record(ctx, "SearchRemoteJobs", in))
}
func (f *fakeJobSearch) RecoJobs(ctx context.Context, in *jobv3.RecoJobsRequest, _ ...grpc.CallOption) (*jobv3.SearchJobsResponse, error) {
	return typed[*jobv3.SearchJobsResponse](f.record(ctx, "RecoJobs", in))
}
func (f *fakeJobSearch) SearchSalary(ctx context.Context, in *jobv3.SearchSalaryRequest, _ ...grpc.CallOption) (*jobv3.SearchSalaryResponse, error) {
	return typed[*jobv3.SearchSalaryResponse](f.record(ctx, "SearchSalary", in))
}

type fakeProfiles struct {
	jobseekerv3.JobseekerProfileServiceClient
	*recorder
}

func (f *fakeProfiles) GetMyProfile(ctx context.Context, in *jobseekerv3.GetMyProfileRequest, _ ...grpc.CallOption) (*jobseekerv3.GetMyProfileResponse, error) {
	return typed[*jobseekerv3.GetMyProfileResponse](f.record(ctx, "GetMyProfile", in))
}
func (f *fakeProfiles) GetProfileCompletion(ctx context.Context, in *jobseekerv3.GetProfileCompletionRequest, _ ...grpc.CallOption) (*jobseekerv3.GetProfileCompletionResponse, error) {
	return typed[*jobseekerv3.GetProfileCompletionResponse](f.record(ctx, "GetProfileCompletion", in))
}

type fakePreferences struct {
	jobseekerv3.JobseekerPreferenceServiceClient
	*recorder
}

func (f *fakePreferences) ListJobPreferences(ctx context.Context, in *jobseekerv3.ListJobPreferencesRequest, _ ...grpc.CallOption) (*jobseekerv3.ListJobPreferencesResponse, error) {
	return typed[*jobseekerv3.ListJobPreferencesResponse](f.record(ctx, "ListJobPreferences", in))
}
func (f *fakePreferences) JobMatchEvaluate(ctx context.Context, in *jobseekerv3.JobMatchEvaluateRequest, _ ...grpc.CallOption) (*jobseekerv3.JobMatchEvaluateResponse, error) {
	return typed[*jobseekerv3.JobMatchEvaluateResponse](f.record(ctx, "JobMatchEvaluate", in))
}

type fakeConfig struct {
	configv3.ConfigServiceClient
	*recorder
}

func (f *fakeConfig) GetCountryConfig(ctx context.Context, in *configv3.GetCountryConfigRequest, _ ...grpc.CallOption) (*configv3.GetCountryConfigResponse, error) {
	return typed[*configv3.GetCountryConfigResponse](f.record(ctx, "GetCountryConfig", in))
}

type fakeConfigSearch struct {
	configv3.ConfigSearchServiceClient
	*recorder
}

func (f *fakeConfigSearch) SearchJobTitleLists(ctx context.Context, in *configv3.SearchJobTitleListsRequest, _ ...grpc.CallOption) (*configv3.SearchJobTitleListsResponse, error) {
	return typed[*configv3.SearchJobTitleListsResponse](f.record(ctx, "SearchJobTitleLists", in))
}

// env 测试环境：DI 容器 + 下游 fake（通过 Decorate 替换）+ 测试公钥。
type env struct {
	c   *dig.Container
	rec *recorder
}

func newEnv(t *testing.T) *env {
	t.Helper()
	c := di.NewContainer()
	rec := newRecorder()
	decorations := []any{
		func(oauthrs.KeySource) oauthrs.KeySource { return staticKeys{testKid: &signingKey(t).PublicKey} },
		func(jobv3.JobServiceClient) jobv3.JobServiceClient { return &fakeJobs{recorder: rec} },
		func(jobv3.JobSearchServiceClient) jobv3.JobSearchServiceClient { return &fakeJobSearch{recorder: rec} },
		func(jobseekerv3.JobseekerProfileServiceClient) jobseekerv3.JobseekerProfileServiceClient {
			return &fakeProfiles{recorder: rec}
		},
		func(jobseekerv3.JobseekerPreferenceServiceClient) jobseekerv3.JobseekerPreferenceServiceClient {
			return &fakePreferences{recorder: rec}
		},
		func(configv3.ConfigServiceClient) configv3.ConfigServiceClient { return &fakeConfig{recorder: rec} },
		func(configv3.ConfigSearchServiceClient) configv3.ConfigSearchServiceClient {
			return &fakeConfigSearch{recorder: rec}
		},
	}
	for _, d := range decorations {
		if err := c.Decorate(d); err != nil {
			t.Fatalf("decorate: %v", err)
		}
	}
	rec.set("GetCountryConfig", countryConfigFixture(t), nil)
	return &env{c: c, rec: rec}
}

// server 启动真实 HTTP 入口（鉴权中间件 + Streamable HTTP）。
func (e *env) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(digutil.MustGet[*app.MCPApp](e.c).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// client 以指定 Token 建立 MCP 会话并完成 initialize。
func (e *env) client(t *testing.T, token string) *client.Client {
	t.Helper()
	srv := e.server(t)
	cli, err := client.NewStreamableHttpClient(srv.URL+"/jobseeker/mcp",
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + token}))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	ctx := context.Background()
	if err := cli.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "citest", Version: "1"}
	if _, err := cli.Initialize(ctx, req); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return cli
}

// callTool 调用工具；成功时把结构化结果解码到 out（protojson）。
func callTool(t *testing.T, cli *client.Client, name string, args map[string]any, out proto.Message) (*mcp.CallToolResult, string) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := cli.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			text = tc.Text
		}
	}
	if !res.IsError && out != nil {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(text), out); err != nil {
			t.Fatalf("decode %s result %q: %v", name, text, err)
		}
	}
	return res, text
}

func countryConfigFixture(t *testing.T) *configv3.GetCountryConfigResponse {
	raw := `{
	  "xp_lvls": [{"id": 1, "key": "no_experience", "value": "No Experience", "seo-value": "no-experience"},
	              {"id": 2, "key": "1_3_yrs", "value": "1 - 3 Yrs", "seo-value": "1-3-yrs"}],
	  "location_lists": [
	    {"id": 10, "key": "central_visayas", "value": "Central Visayas", "display_name": "Central Visayas",
	     "locations": [{"id": 101, "key": "cebu", "value": "Cebu"}]},
	    {"id": 20, "key": "metro_manila", "value": "Metro Manila", "display_name": "Metro Manila",
	     "locations": [{"id": 201, "key": "makati", "value": "Makati"}]}
	  ]
	}`
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("fixture struct: %v", err)
	}
	return &configv3.GetCountryConfigResponse{Data: s}
}

func mcpListToolsRequest() mcp.ListToolsRequest { return mcp.ListToolsRequest{} }
