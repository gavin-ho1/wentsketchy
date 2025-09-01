package items

import (
	"context"
	"log/slog"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/internal/aerospace/events"
)

type AerospaceJob struct {
	logger  *slog.Logger
	updater Updater
}

func NewAerospaceJob(logger *slog.Logger, updater Updater) *AerospaceJob {
	return &AerospaceJob{
		logger:  logger,
		updater: updater,
	}
}

func (j *AerospaceJob) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				j.logger.InfoContext(ctx, "aerospace job: refreshing")
				err := j.updater.Update(ctx, &args.In{
					Name:  AerospaceName,
					Event: events.AerospaceRefresh,
				})
				if err != nil {
					j.logger.ErrorContext(ctx, "aerospace job: failed to refresh", slog.Any("error", err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
