package main

import (
	"os"

	"github.com/lucax88x/wentsketchy/cmd/cli/config"
	"github.com/lucax88x/wentsketchy/cmd/cli/console"
	"github.com/lucax88x/wentsketchy/internal/setup"
	"github.com/spf13/viper"
)

func cli(viper *viper.Viper, console *console.Console, cfg *config.Cfg) setup.ProgramExecutor {
	return setup.NewCliExecutor(viper, console, cfg)
}

func main() {
	result := setup.Run(cli)

	if result == setup.NotOk {
		os.Exit(1)
	}
}
