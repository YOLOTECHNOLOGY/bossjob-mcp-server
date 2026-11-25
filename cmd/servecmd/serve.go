package servecmd

import (
	"bossjob-mcp-server/app"

	"github.com/spf13/cobra"
	"go.uber.org/dig"
)

// NewCommand serve：启动 MCP HTTP 服务。
func NewCommand(c *dig.Container) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "serve jobseeker MCP over streamable HTTP",
		RunE: func(_ *cobra.Command, _ []string) error {
			return c.Invoke(func(a *app.MCPApp) error { return a.Start() })
		},
	}
}
