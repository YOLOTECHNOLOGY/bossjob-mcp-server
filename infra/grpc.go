package infra

import (
	"fmt"

	"bossjob-mcp-server/conf"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	jobv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/job/v3"
	jobseekerv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/jobseeker/v3"
	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/grpc/grpchelper"
	"go.uber.org/dig"
	"google.golang.org/grpc"
)

// Conns 下游领域服务 gRPC 连接（grpc.NewClient 惰性建连，启动时不依赖下游可用）。
type Conns struct {
	Job       *grpc.ClientConn
	Jobseeker *grpc.ClientConn
	Config    *grpc.ClientConn
}

// NewConns 按配置创建下游连接；拦截器 climw.CtxMD 透传 CtxInfo（x-user-id / x-ctxinfo-bin）。
func NewConns(cfg conf.GRPCConfig) (*Conns, error) {
	dial := func(name, target string) (*grpc.ClientConn, error) {
		if target == "" {
			return nil, fmt.Errorf("map_config.grpc.%s is required", name)
		}
		cc, err := grpc.NewClient(target, grpchelper.DefaultClientOpts()...)
		if err != nil {
			return nil, fmt.Errorf("dial %s grpc %s: %w", name, target, err)
		}
		return cc, nil
	}
	job, err := dial("job", cfg.Job)
	if err != nil {
		return nil, err
	}
	jobseeker, err := dial("jobseeker", cfg.Jobseeker)
	if err != nil {
		return nil, err
	}
	config, err := dial("config", cfg.Config)
	if err != nil {
		return nil, err
	}
	return &Conns{Job: job, Jobseeker: jobseeker, Config: config}, nil
}

// Clients 下游 gRPC 客户端，按接口类型注册到 DI，测试通过 Decorate 替换。
type Clients struct {
	dig.Out

	Jobs        jobv3.JobServiceClient
	JobSearch   jobv3.JobSearchServiceClient
	Profiles    jobseekerv3.JobseekerProfileServiceClient
	Preferences jobseekerv3.JobseekerPreferenceServiceClient
	Config      configv3.ConfigServiceClient
	ConfigSrch  configv3.ConfigSearchServiceClient
}

// NewClients 基于连接创建各领域服务客户端。
func NewClients(c *Conns) Clients {
	return Clients{
		Jobs:        jobv3.NewJobServiceClient(c.Job),
		JobSearch:   jobv3.NewJobSearchServiceClient(c.Job),
		Profiles:    jobseekerv3.NewJobseekerProfileServiceClient(c.Jobseeker),
		Preferences: jobseekerv3.NewJobseekerPreferenceServiceClient(c.Jobseeker),
		Config:      configv3.NewConfigServiceClient(c.Config),
		ConfigSrch:  configv3.NewConfigSearchServiceClient(c.Config),
	}
}
