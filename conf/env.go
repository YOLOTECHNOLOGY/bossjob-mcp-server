package conf

import "strings"

// DeploymentEnv 返回配置中的部署环境。
func DeploymentEnv() string {
	return strings.TrimSpace(Settings.KeyConfig.Env)
}

// IsProductionEnv 是否为生产环境。
func IsProductionEnv() bool {
	return DeploymentEnv() == "production"
}
