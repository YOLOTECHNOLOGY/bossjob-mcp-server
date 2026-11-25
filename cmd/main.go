package main

import (
	"log"

	"bossjob-mcp-server/conf"
	"bossjob-mcp-server/di"
	"bossjob-mcp-server/pub/digutil"

	"github.com/spf13/cobra"
	"go.uber.org/dig"
)

func main() {
	rootCmd := &cobra.Command{Short: "bossjob-mcp-server"}
	c := di.NewContainer()
	for _, cmd := range digutil.MustGet[CmdParam](c).Cmds {
		rootCmd.AddCommand(cmd)
	}
	rootCmd.PersistentFlags().StringVarP(&conf.ConfigFile, "config", "c", "", "config file, eg: config.yaml")
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("err: %v", err)
	}
}

// CmdParam 收集 group:"cmd" 下的全部子命令。
type CmdParam struct {
	dig.In

	Cmds []*cobra.Command `group:"cmd"`
}
