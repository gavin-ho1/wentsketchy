package command

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

type Command struct {
	logger *slog.Logger
}

func NewCommand(logger *slog.Logger) *Command {
	return &Command{
		logger,
	}
}

func (c Command) Run(ctx context.Context, name string, arg ...string) (string, error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		c.logger.DebugContext(ctx, "command: took", slog.String("name", name), slog.Duration("elapsed", elapsed))
	}()

	cmd := exec.CommandContext(ctx, name, arg...)

	c.logger.DebugContext(ctx, "command: env", slog.Any("env", cmd.Env))

	// fmt.Println(fmt.Sprintf("%s %v", cmd.Path, cmd.Args))

	out, err := cmd.Output()

	if err != nil {
		//nolint:errorlint // no wrap
		return "", fmt.Errorf("could not run command '%s'. %v", name, err)
	}

	return string(out), nil
}

func (c Command) RunBufferized(ctx context.Context, name string, arg ...string) (bytes.Buffer, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()

	if err != nil {
		return bytes.Buffer{}, fmt.Errorf("could not run command %w", err)
	}

	return out, nil
}
