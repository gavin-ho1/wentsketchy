package items

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/jobs"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
)

type BluetoothJob struct {
	logger     *slog.Logger
	command    *command.Command
	sketchybar sketchybar.API
}

func NewBluetoothJob(logger *slog.Logger, command *command.Command, sketchybar sketchybar.API) *BluetoothJob {
	return &BluetoothJob{logger, command, sketchybar}
}

func (j *BluetoothJob) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				j.logger.ErrorContext(ctx, "bluetooth job: recovered from panic", slog.Any("panic", r))
				time.Sleep(time.Second * 5)
				j.logger.InfoContext(ctx, "bluetooth job: restarting after panic")
				j.Start(ctx)
			}
		}()

		var lastStatus string
		ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
		defer ticker.Stop()

		// Initial check
		output, err := j.command.Run(ctx, "blueutil", "-p")
		if err != nil {
			j.logger.Error("bluetooth job: could not get initial bluetooth status", "error", err)
		}
		lastStatus = strings.TrimSpace(output)
		// Trigger a refresh on start, so the label is correct
		err = j.sketchybar.Run(ctx, []string{"--trigger", "bluetooth_change"})
		if err != nil {
			j.logger.Error("bluetooth job: could not trigger initial event", "error", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				output, err := j.command.Run(ctx, "blueutil", "-p")
				if err != nil {
					j.logger.Error("bluetooth job: could not get bluetooth status", "error", err)
					continue
				}

				currentStatus := strings.TrimSpace(output)
				if currentStatus != lastStatus {
					err := j.sketchybar.Run(ctx, []string{"--trigger", "bluetooth_change"})
					if err != nil {
						j.logger.Error("bluetooth job: could not trigger event", "error", err)
					}
				}
				lastStatus = currentStatus
			}
		}
	}()
}

var _ jobs.Job = (*BluetoothJob)(nil)