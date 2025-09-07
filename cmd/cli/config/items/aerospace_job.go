package items

import (
	"context"
	"log/slog"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/internal/aerospace/events"
	"github.com/lucax88x/wentsketchy/internal/jobs"
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
		defer func() {
			if r := recover(); r != nil {
				j.logger.ErrorContext(ctx, "aerospace job: recovered from panic", slog.Any("panic", r))
				// Restart the job after a panic
				time.Sleep(time.Second * 5)
				j.logger.InfoContext(ctx, "aerospace job: restarting after panic")
				j.Start(ctx)
			}
		}()

		j.logger.InfoContext(ctx, "aerospace job: starting")
		
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// Keep track of consecutive failures for backoff
		consecutiveFailures := 0
		maxConsecutiveFailures := 5
		baseDelay := time.Second * 2

		for {
			select {
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							j.logger.ErrorContext(ctx, "aerospace job: recovered from panic during refresh", slog.Any("panic", r))
							consecutiveFailures++
						}
					}()

					j.logger.DebugContext(ctx, "aerospace job: refreshing")
					
					err := j.updater.Update(ctx, &args.In{
						Name:  AerospaceName,
						Event: events.AerospaceRefresh,
					})
					
					if err != nil {
						consecutiveFailures++
						j.logger.ErrorContext(ctx, "aerospace job: failed to refresh", 
							slog.Any("error", err),
							slog.Int("consecutiveFailures", consecutiveFailures))
						
						// Implement exponential backoff for consecutive failures
						if consecutiveFailures >= maxConsecutiveFailures {
							backoffDelay := baseDelay * time.Duration(consecutiveFailures-maxConsecutiveFailures+1)
							if backoffDelay > time.Minute {
								backoffDelay = time.Minute
							}
							j.logger.WarnContext(ctx, "aerospace job: too many consecutive failures, backing off", 
								slog.Duration("backoffDelay", backoffDelay))
							
							select {
							case <-ctx.Done():
								return
							case <-time.After(backoffDelay):
							}
						}
					} else {
						// Success - reset failure counter
						if consecutiveFailures > 0 {
							j.logger.InfoContext(ctx, "aerospace job: refresh succeeded after failures", 
								slog.Int("previousFailures", consecutiveFailures))
						}
						consecutiveFailures = 0
					}
				}()
				
			case <-ctx.Done():
				j.logger.InfoContext(ctx, "aerospace job: stopping due to context cancellation")
				return
			}
		}
	}()
}

var _ jobs.Job = (*AerospaceJob)(nil)
