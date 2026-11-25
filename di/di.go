package di

import (
	"bossjob-mcp-server/app"
	"bossjob-mcp-server/cmd/servecmd"
	"bossjob-mcp-server/conf"
	"bossjob-mcp-server/domain/jobseeker"
	"bossjob-mcp-server/domain/oauthrs"
	"bossjob-mcp-server/infra"

	"go.uber.org/dig"
)

// NewContainer 组合根：配置、下游 client、OAuth 校验、领域服务、应用与命令统一在此注册。
func NewContainer() *dig.Container {
	c := dig.New()
	c.Provide(func() *dig.Container { return c })

	// config
	c.Provide(func() conf.Conf { conf.InitConf(); return conf.Settings })
	c.Provide(func(cfg conf.Conf) conf.OAuthConfig { return cfg.MapConfig.OAuth })
	c.Provide(func(cfg conf.Conf) conf.GRPCConfig { return cfg.MapConfig.GRPC })
	c.Provide(func(cfg conf.Conf) conf.MCPConfig { return cfg.MapConfig.MCP })

	// infra
	c.Provide(infra.NewConns)
	c.Provide(infra.NewClients)

	// oauth resource server
	c.Provide(func(cfg conf.OAuthConfig) oauthrs.KeySource {
		return oauthrs.NewJWKSKeySource(cfg.JWKSURI(), cfg.JWKSRefreshInterval, nil)
	})
	c.Provide(func(cfg conf.OAuthConfig, keys oauthrs.KeySource) oauthrs.Verifier {
		return oauthrs.NewVerifier(oauthrs.VerifierConfig{
			Issuer: cfg.Issuer, Resource: cfg.JobseekerResource, Role: oauthrs.RoleJobseeker, ClockSkew: cfg.ClockSkew,
		}, keys)
	})

	// domain
	c.Provide(func(g conf.GRPCConfig, m conf.MCPConfig) jobseeker.Options {
		return jobseeker.Options{WebBaseURL: m.WebBaseURL, Timeout: g.Timeout}
	})
	c.Provide(jobseeker.NewResolver)
	c.Provide(jobseeker.NewService)

	// app
	c.Provide(app.NewMCPApp)

	// commands
	provideCmd(c, servecmd.NewCommand)

	return c
}

func provideCmd(c *dig.Container, constructor any) {
	c.Provide(constructor, dig.Group("cmd"))
}
