package conf

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var initOnce sync.Once

// ConfigFile 通过 -c 指定的配置文件路径。
var ConfigFile string

// InitConf 加载配置，优先级：-c 指定文件 > {ENV}_config.yaml > config.yaml。
func InitConf() {
	initOnce.Do(func() {
		filename := ConfigFile
		if filename == "" {
			filename = fmt.Sprintf("%s_config.yaml", os.Getenv("ENV"))
			if _, err := os.Stat(filename); err != nil && os.IsNotExist(err) {
				filename = "config.yaml"
			}
		}
		log.Printf("load config: %v", filename)
		bs, err := os.ReadFile(filename)
		if err != nil {
			log.Fatalf("Error reading config file: %v", err)
		}
		if err := yaml.Unmarshal(bs, &Settings); err != nil {
			log.Fatalf("Unable to unmarshal config: %v", err)
		}
		applyDefaults(&Settings)
	})
}

func applyDefaults(c *Conf) {
	if c.KeyConfig.Port == 0 {
		c.KeyConfig.Port = 8000
	}
	if c.MapConfig.OAuth.JWKSRefreshInterval <= 0 {
		c.MapConfig.OAuth.JWKSRefreshInterval = time.Minute
	}
	if c.MapConfig.OAuth.ClockSkew <= 0 {
		c.MapConfig.OAuth.ClockSkew = 30 * time.Second
	}
	if c.MapConfig.GRPC.Timeout <= 0 {
		c.MapConfig.GRPC.Timeout = 5 * time.Second
	}
	if c.MapConfig.MCP.JobseekerPath == "" {
		c.MapConfig.MCP.JobseekerPath = "/jobseeker/mcp"
	}
}
