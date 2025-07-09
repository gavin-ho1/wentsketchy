package items

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
)

type WifiJob struct {
	logger     *slog.Logger
	command    *command.Command
	sketchybar sketchybar.API
}

func NewWifiJob(logger *slog.Logger, command *command.Command, sketchybar sketchybar.API) *WifiJob {
	return &WifiJob{logger, command, sketchybar}
}

func (j *WifiJob) Start(ctx context.Context) {
	go func() {
		var lastStatus string
		ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
		defer ticker.Stop()

		// Initial check
		output, err := j.command.Run(ctx, "networksetup", "-getairportpower", "en0")
		if err != nil {
			j.logger.Error("wifi job: could not get initial wifi status", "error", err)
		}
		lastStatus = strings.TrimSpace(output)
		// Trigger a refresh on start, so the label is correct
		err = j.sketchybar.Run(ctx, []string{"--trigger", "wifi_change"})
		if err != nil {
			j.logger.Error("wifi job: could not trigger initial event", "error", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				output, err := j.command.Run(ctx, "networksetup", "-getairportpower", "en0")
				if err != nil {
					j.logger.Error("wifi job: could not get wifi status", "error", err)
					continue
				}

				currentStatus := strings.TrimSpace(output)
				if currentStatus != lastStatus {
					err := j.sketchybar.Run(ctx, []string{"--trigger", "wifi_change"})
					if err != nil {
						j.logger.Error("wifi job: could not trigger event", "error", err)
					}
				}
				lastStatus = currentStatus
			}
		}
	}()
}
