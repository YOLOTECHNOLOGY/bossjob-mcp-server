package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bossjob-mcp-server/conf"
	"bossjob-mcp-server/domain/jobseeker"
	"bossjob-mcp-server/domain/oauthrs"
	"bossjob-mcp-server/domain/recruiter"

	"github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1/openv1mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/redpanda-data/protoc-gen-go-mcp/pkg/runtime"
	"github.com/redpanda-data/protoc-gen-go-mcp/pkg/runtime/mark3labs"
	"go.uber.org/dig"
)

// ServerVersion MCP initialize 返回的服务版本。
const ServerVersion = "1.0.0"

// MCPApp Bossjob MCP HTTP 应用：求职者 / 招聘者两个 Streamable HTTP（无状态）端点，
// 各自独立的 OAuth Bearer 校验（resource + role）与 RFC 9728 元数据。
type MCPApp struct {
	port    int
	handler http.Handler
}

// MCPAppParams 依赖。
type MCPAppParams struct {
	dig.In

	Conf      conf.Conf
	Verifiers oauthrs.Verifiers
	Jobseeker *jobseeker.Service
	Recruiter *recruiter.Service
}

// endpoint 单个 MCP 端点（一个 OAuth resource）。
type endpoint struct {
	serverName   string
	resourceName string
	resource     string
	path         string
	instructions string
	verifier     oauthrs.Verifier
	register     func(registrar runtime.MCPServer)
}

// NewMCPApp 组装路由并校验配置。
func NewMCPApp(p MCPAppParams) (*MCPApp, error) {
	oauth, mcpCfg := p.Conf.MapConfig.OAuth, p.Conf.MapConfig.MCP
	if err := validateConfig(oauth, mcpCfg, conf.IsProductionEnv()); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	endpoints := []endpoint{
		{
			serverName: "bossjob-jobseeker", resourceName: "Bossjob Jobseeker",
			resource: oauth.JobseekerResource, path: mcpCfg.JobseekerPath, verifier: p.Verifiers.Jobseeker,
			instructions: "Bossjob tools for the signed-in jobseeker: search and inspect jobs, get recommendations, " +
				"review the user's profile, applications and saved jobs, save/unsave jobs and look up salary insights. " +
				"Show job_url links so the user can view or apply on Bossjob.",
			register: func(r runtime.MCPServer) { openv1mcp.RegisterJobseekerMcpServiceHandler(r, p.Jobseeker) },
		},
		{
			serverName: "bossjob-recruiter", resourceName: "Bossjob Recruiter",
			resource: oauth.RecruiterResource, path: mcpCfg.RecruiterPath, verifier: p.Verifiers.Recruiter,
			instructions: "Bossjob tools for the signed-in recruiter: review the recruiter's account, credits and jobs, " +
				"search the talent pool and recommended talents for a job, inspect talent profiles (names are masked and " +
				"contact details are never returned), list candidates who interacted with the recruiter, save/unsave talents, " +
				"and check interviews, recruitment statistics and market salaries. These tools never spend credits or message " +
				"jobseekers: to unlock, chat with or interview a talent, ask the user to continue in Bossjob.",
			register: func(r runtime.MCPServer) { openv1mcp.RegisterRecruiterMcpServiceHandler(r, p.Recruiter) },
		},
	}
	for _, ep := range endpoints {
		mount(mux, ep, oauth.Issuer, mcpCfg.AllowedHosts)
	}
	return &MCPApp{port: p.Conf.KeyConfig.Port, handler: mux}, nil
}

func mount(mux *http.ServeMux, ep endpoint, issuer string, allowedHosts []string) {
	mcpServer, registrar := mark3labs.NewServer(ep.serverName, ServerVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(ep.instructions),
	)
	ep.register(registrar)

	opts := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithEndpointPath(ep.path),
	}
	if len(allowedHosts) > 0 {
		opts = append(opts, server.WithAllowedHosts(allowedHosts...))
	}
	streamable := server.NewStreamableHTTPServer(mcpServer, opts...)

	metadataPath := server.ProtectedResourceMetadataPath(ep.resource)
	auth := oauthrs.NewMiddleware(ep.verifier, oauthrs.ResourceMetadataURL(ep.resource, metadataPath))
	mux.Handle(metadataPath, server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource:               ep.resource,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        []string{oauthrs.AccessScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           ep.resourceName,
	}))
	mux.Handle(ep.path, auth.Wrap(streamable))
}

// Handler 返回 HTTP 处理器（测试使用 httptest 挂载）。
func (a *MCPApp) Handler() http.Handler { return a.handler }

// Start 启动 HTTP 服务。
func (a *MCPApp) Start() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.port),
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("mcp-server listening on %s", srv.Addr)
	return srv.ListenAndServe()
}

func validateConfig(oauth conf.OAuthConfig, mcpCfg conf.MCPConfig, production bool) error {
	issuer, err := url.Parse(oauth.Issuer)
	if err != nil || issuer.Host == "" || strings.TrimRight(issuer.Path, "/") != "" || issuer.RawQuery != "" {
		return errors.New("map_config.oauth.issuer must be an origin such as https://dev.auth.bossjob.com")
	}
	if production && issuer.Scheme != "https" {
		return errors.New("issuer must use https in production")
	}
	for _, r := range []struct{ name, resource, pathName, path string }{
		{"jobseeker_resource", oauth.JobseekerResource, "jobseeker_path", mcpCfg.JobseekerPath},
		{"recruiter_resource", oauth.RecruiterResource, "recruiter_path", mcpCfg.RecruiterPath},
	} {
		resource, err := url.Parse(r.resource)
		if err != nil || resource.Host == "" || resource.RawQuery != "" || resource.Fragment != "" {
			return fmt.Errorf("map_config.oauth.%s must be an absolute URL", r.name)
		}
		if resource.Path != r.path {
			return fmt.Errorf("map_config.mcp.%s %q must equal the path of %s %q", r.pathName, r.path, r.name, resource.Path)
		}
		if production && resource.Scheme != "https" {
			return fmt.Errorf("%s must use https in production", r.name)
		}
	}
	if mcpCfg.JobseekerPath == mcpCfg.RecruiterPath {
		return errors.New("map_config.mcp.jobseeker_path and recruiter_path must differ")
	}
	return nil
}
