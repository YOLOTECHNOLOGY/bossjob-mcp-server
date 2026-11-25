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

	"github.com/YOLOTECHNOLOGY/bossjob-api-public/go/open/v1/openv1mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/redpanda-data/protoc-gen-go-mcp/pkg/runtime/mark3labs"
	"go.uber.org/dig"
)

// ServerName / ServerVersion MCP initialize 返回的服务信息。
const (
	ServerName    = "bossjob-jobseeker"
	ServerVersion = "1.0.0"
)

// MCPApp 求职者 MCP HTTP 应用：Streamable HTTP（无状态）+ OAuth Bearer 校验 + RFC 9728 元数据。
type MCPApp struct {
	port    int
	handler http.Handler
}

// MCPAppParams 依赖。
type MCPAppParams struct {
	dig.In

	Conf      conf.Conf
	Verifier  oauthrs.Verifier
	Jobseeker *jobseeker.Service
}

// NewMCPApp 组装路由并校验配置。
func NewMCPApp(p MCPAppParams) (*MCPApp, error) {
	oauth, mcpCfg := p.Conf.MapConfig.OAuth, p.Conf.MapConfig.MCP
	if err := validateConfig(oauth, mcpCfg, conf.IsProductionEnv()); err != nil {
		return nil, err
	}

	mcpServer, registrar := mark3labs.NewServer(ServerName, ServerVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Bossjob tools for the signed-in jobseeker: search and inspect jobs, get recommendations, "+
			"review the user's profile, applications and saved jobs, save/unsave jobs and look up salary insights. "+
			"Show job_url links so the user can view or apply on Bossjob."),
	)
	openv1mcp.RegisterJobseekerMcpServiceHandler(registrar, p.Jobseeker)

	opts := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithEndpointPath(mcpCfg.JobseekerPath),
	}
	if len(mcpCfg.AllowedHosts) > 0 {
		opts = append(opts, server.WithAllowedHosts(mcpCfg.AllowedHosts...))
	}
	streamable := server.NewStreamableHTTPServer(mcpServer, opts...)

	metadataPath := server.ProtectedResourceMetadataPath(oauth.JobseekerResource)
	auth := oauthrs.NewMiddleware(p.Verifier, oauthrs.ResourceMetadataURL(oauth.JobseekerResource, metadataPath))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.Handle(metadataPath, server.NewProtectedResourceMetadataHandler(server.ProtectedResourceMetadataConfig{
		Resource:               oauth.JobseekerResource,
		AuthorizationServers:   []string{oauth.Issuer},
		ScopesSupported:        []string{oauthrs.AccessScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Bossjob Jobseeker",
	}))
	mux.Handle(mcpCfg.JobseekerPath, auth.Wrap(streamable))

	return &MCPApp{port: p.Conf.KeyConfig.Port, handler: mux}, nil
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
	resource, err := url.Parse(oauth.JobseekerResource)
	if err != nil || resource.Host == "" || resource.RawQuery != "" || resource.Fragment != "" {
		return errors.New("map_config.oauth.jobseeker_resource must be an absolute URL")
	}
	if resource.Path != mcpCfg.JobseekerPath {
		return fmt.Errorf("map_config.mcp.jobseeker_path %q must equal the path of jobseeker_resource %q", mcpCfg.JobseekerPath, resource.Path)
	}
	if production && (issuer.Scheme != "https" || resource.Scheme != "https") {
		return errors.New("issuer and jobseeker_resource must use https in production")
	}
	return nil
}
