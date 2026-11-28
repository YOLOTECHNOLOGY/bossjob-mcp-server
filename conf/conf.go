package conf

import "time"

// Settings 为进程级配置快照，仅由组合根（DI provider）读取并注入，业务代码禁止直接引用。
var Settings Conf

type Conf struct {
	KeyConfig struct {
		Env  string `yaml:"env"`
		Port int    `yaml:"port"`
		Name string `yaml:"name"`
	} `yaml:"key_config"`
	MapConfig struct {
		OAuth OAuthConfig `yaml:"oauth"`
		GRPC  GRPCConfig  `yaml:"grpc"`
		MCP   MCPConfig   `yaml:"mcp"`
	} `yaml:"map_config"`
}

// OAuthConfig 资源服务器（MCP）校验 auth-go 签发的 Access Token 所需配置。
type OAuthConfig struct {
	// Issuer auth-go OAuth issuer，例如 https://dev.auth.bossjob.com。
	Issuer string `yaml:"issuer"`
	// JWKSURL 为空时使用 Issuer + "/.well-known/jwks.json"。
	JWKSURL string `yaml:"jwks_url"`
	// JobseekerResource 求职者 MCP resource，必须与 auth-go clients[].bindings.resource 完全一致。
	JobseekerResource string `yaml:"jobseeker_resource"`
	// RecruiterResource 招聘者 MCP resource，必须与 auth-go clients[].bindings.resource 完全一致。
	RecruiterResource string `yaml:"recruiter_resource"`
	// JWKSRefreshInterval JWKS 最短刷新间隔（遇未知 kid 时也受此限制，防止被刷）。
	JWKSRefreshInterval time.Duration `yaml:"jwks_refresh_interval"`
	// ClockSkew 校验 exp / nbf / iat 时允许的时钟偏差。
	ClockSkew time.Duration `yaml:"clock_skew"`
}

// JWKSURI 返回 JWKS 地址。
func (c OAuthConfig) JWKSURI() string {
	if c.JWKSURL != "" {
		return c.JWKSURL
	}
	return c.Issuer + "/.well-known/jwks.json"
}

// GRPCConfig 下游领域服务 gRPC 地址。
type GRPCConfig struct {
	Job       string `yaml:"job"`
	Jobseeker string `yaml:"jobseeker"`
	Config    string `yaml:"config"`
	Recruiter string `yaml:"recruiter"`
	Talent    string `yaml:"talent"`
	JobApp    string `yaml:"job_app"`
	// Timeout 单次下游调用超时。
	Timeout time.Duration `yaml:"timeout"`
}

// MCPConfig MCP HTTP 入口配置。
type MCPConfig struct {
	// JobseekerPath 求职者 MCP 端点路径，需与 JobseekerResource 的 path 一致。
	JobseekerPath string `yaml:"jobseeker_path"`
	// RecruiterPath 招聘者 MCP 端点路径，需与 RecruiterResource 的 path 一致。
	RecruiterPath string `yaml:"recruiter_path"`
	// AllowedHosts DNS rebinding 防护的 Host 白名单（公网域名）。
	AllowedHosts []string `yaml:"allowed_hosts"`
	// WebBaseURL 职位 / 公司详情链接的站点根地址，例如 https://dev.bossjob.com。
	WebBaseURL string `yaml:"web_base_url"`
}
