package setup

import (
	"context"
	"log/slog"
	"os"
	// "os/signal"
	// "syscall"
	"time"

	"fmt"

	"github.com/lmittmann/tint"
	"github.com/lucax88x/wentsketchy/cmd/cli/config"
	"github.com/lucax88x/wentsketchy/cmd/cli/console"
	"github.com/spf13/viper"
)

type ExecutionResult = int

const (
	Ok    ExecutionResult = 0
	NotOk ExecutionResult = -1
)

func initViper() (*viper.Viper, error) {
	viperInstance := viper.New()

	return viperInstance, nil
}

type ProgramExecutor func(ctx context.Context, logger *slog.Logger) error

type ExecutorBuilder func(
	viper *viper.Viper,
	console *console.Console,
	cfg *config.Cfg,
) ProgramExecutor

func Run(buildExecutor ExecutorBuilder) ExecutionResult {
	start := time.Now()

	cfg, err := config.ReadYaml()
	if err != nil {
		// Cannot create logger yet, so just print to stderr
		fmt.Fprintf(os.Stderr, "main: could not read config for logger: %v\n", err)
		// Fallback to a default logger
	}

	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	logger := slog.New(tint.NewHandler(
		os.Stderr,
		&tint.Options{Level: logLevel},
	))

	defer func() {
		elapsed := time.Since(start)
		logger.Info("cli: took", slog.Duration("elapsed", elapsed))
	}()

	viper, err := initViper()

	if err != nil {
		logger.Error("main: could not setup configuration", slog.Any("err", err))
		return NotOk
	}

	console := &console.Console{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	ctx := context.Background()
	err = buildExecutor(viper, console, cfg)(ctx, logger)

	if err != nil {
		logger.Error("main: failed to execute program", slog.Any("err", err))
		return NotOk
	}

	logger.Debug("main: completed", slog.Int("status_code", Ok))

	return Ok
}
