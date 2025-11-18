package commands

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/console"
	"github.com/lucax88x/wentsketchy/cmd/cli/runner"
	"github.com/lucax88x/wentsketchy/internal/wentsketchy"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewStartCmd(
	ctx context.Context,
	logger *slog.Logger,
	viper *viper.Viper,
	console *console.Console,
	cfg *config.Cfg,
) *cobra.Command {
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "start wentsketchy",
		RunE: func(_ *cobra.Command, args []string) error {
			return runner.RunCmdE(ctx, logger, viper, console, args, cfg, runStartCmd())
		},
	}

	startCmd.SetOut(console.Stdout)
	startCmd.SetErr(console.Stderr)

	return startCmd
}

func runStartCmd() runner.RunE {
	return func(
		ctx context.Context,
		_ *console.Console,
		_ []string,
		di *wentsketchy.Wentsketchy,
	) error {
		// Create PID file with error handling that doesn't exit
		if err := runner.CreatePidFile(settings.PidFilePath); err != nil {
			di.Logger.ErrorContext(ctx, "start: could not create pid file, continuing anyway", slog.Any("error", err))
		}

		defer func() {
			if err := runner.RemovePidFile(settings.PidFilePath); err != nil {
				di.Logger.ErrorContext(ctx, "start: could not remove pid file", slog.Any("error", err))
			}
		}()

		// Start FIFO with retry mechanism
		startFifoWithRetry(ctx, di)

		// Refresh aerospace tree - don't fail if it errors
		di.Logger.InfoContext(ctx, "start: refresh aerospace tree")
		di.Aerospace.SingleFlightRefreshTree()

		// Initialize config with error handling
		di.Logger.InfoContext(ctx, "start: config init")
		if err := di.Config.Init(ctx); err != nil {
			di.Logger.ErrorContext(ctx, "start: config init failed, continuing anyway", slog.Any("error", err))
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Run server and jobs with error recovery
		go runServerWithRecovery(ctx, di, &wg)
		go runJobsWithRecovery(ctx, di, &wg)

		// Wait for shutdown signal
		wg.Wait()

		di.Logger.InfoContext(ctx, "start: shutdown complete")

		// Never return an error - always continue running or exit gracefully
		return nil
	}
}

func startFifoWithRetry(ctx context.Context, di *wentsketchy.Wentsketchy) {
	maxRetries := 5
	retryDelay := time.Second * 2

	for attempt := 1; attempt <= maxRetries; attempt++ {
		di.Logger.InfoContext(
			ctx,
			"start: starting fifo",
			slog.String("path", settings.FifoPath),
			slog.Int("attempt", attempt),
		)

		if err := di.Fifo.Start(settings.FifoPath); err != nil {
			di.Logger.ErrorContext(ctx, "start: could not start fifo", 
				slog.Any("error", err),
				slog.Int("attempt", attempt),
				slog.Int("maxRetries", maxRetries))
			
			if attempt < maxRetries {
				di.Logger.InfoContext(ctx, "start: retrying fifo start", slog.Duration("delay", retryDelay))
				time.Sleep(retryDelay)
				continue
			} else {
				di.Logger.ErrorContext(ctx, "start: fifo failed to start after all retries, continuing without fifo")
			}
		} else {
			di.Logger.InfoContext(ctx, "start: fifo started successfully")
			break
		}
	}
}

func runServerWithRecovery(
	ctx context.Context,
	di *wentsketchy.Wentsketchy,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			di.Logger.ErrorContext(ctx, "server: recovered from panic", slog.Any("panic", r))
		}
	}()

	di.Logger.InfoContext(ctx, "server: starting")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start server with continuous restart on failure
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			select {
			case <-cancelCtx.Done():
				return
			default:
				func() {
					defer func() {
						if r := recover(); r != nil {
							di.Logger.ErrorContext(cancelCtx, "server: recovered from server panic", slog.Any("panic", r))
						}
					}()
					
					di.Logger.InfoContext(cancelCtx, "server: starting server instance")
					di.Server.Start(cancelCtx)
					di.Logger.InfoContext(cancelCtx, "server: server instance stopped")
				}()
				
				// If server exits, wait a bit before restarting
				select {
				case <-cancelCtx.Done():
					return
				case <-time.After(time.Second * 5):
					di.Logger.InfoContext(cancelCtx, "server: restarting server after failure")
				}
			}
		}
	}()

	// Wait for shutdown signal or server completion
	select {
	case <-quit:
		di.Logger.InfoContext(ctx, "server: received shutdown signal")
	case <-serverDone:
		di.Logger.InfoContext(ctx, "server: server goroutine completed")
	}

	cancel()
	di.Logger.InfoContext(ctx, "server: shutdown")
}

func runJobsWithRecovery(
	ctx context.Context,
	di *wentsketchy.Wentsketchy,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			di.Logger.ErrorContext(ctx, "jobs: recovered from panic", slog.Any("panic", r))
		}
	}()

	di.Logger.InfoContext(ctx, "jobs: starting")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)

	tickerCtx, tickerCancel := context.WithCancel(ctx)
	defer tickerCancel()

	// Start periodic jobs with error recovery
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		defer func() {
			if r := recover(); r != nil {
				di.Logger.ErrorContext(tickerCtx, "jobs: recovered from jobs panic", slog.Any("panic", r))
			}
		}()

		ticker := time.NewTicker(time.Minute) // Periodic health check/maintenance
		defer ticker.Stop()

		for {
			select {
			case <-tickerCtx.Done():
				return
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							di.Logger.ErrorContext(tickerCtx, "jobs: recovered from periodic job panic", slog.Any("panic", r))
						}
					}()
					
					// Periodic maintenance tasks
					di.Logger.DebugContext(tickerCtx, "jobs: running periodic maintenance")
					
					// Refresh aerospace tree periodically
					di.Aerospace.SingleFlightRefreshTree()
				}()
			}
		}
	}()

	// Wait for shutdown signal or jobs completion
	select {
	case <-quit:
		di.Logger.InfoContext(ctx, "jobs: received shutdown signal")
	case <-jobsDone:
		di.Logger.InfoContext(ctx, "jobs: jobs goroutine completed")
	}

	tickerCancel()
	di.Logger.InfoContext(ctx, "jobs: shutdown")
}