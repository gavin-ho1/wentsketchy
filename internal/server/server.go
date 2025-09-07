package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/items"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/internal/aerospace"
	"github.com/lucax88x/wentsketchy/internal/aerospace/events"
	"github.com/lucax88x/wentsketchy/internal/fifo"
)

type FifoServer struct {
	logger    *slog.Logger
	config    *config.Config
	fifo      *fifo.Reader
	aerospace aerospace.Aerospace
}

func NewFifoServer(
	logger *slog.Logger,
	config *config.Config,
	fifo *fifo.Reader,
	aerospace aerospace.Aerospace,
) *FifoServer {
	return &FifoServer{
		logger,
		config,
		fifo,
		aerospace,
	}
}

func (f FifoServer) Start(ctx context.Context) {
	// Add recovery mechanism for the entire server
	defer func() {
		if r := recover(); r != nil {
			f.logger.ErrorContext(ctx, "server: recovered from panic in Start", slog.Any("panic", r))
		}
	}()

	f.logger.InfoContext(ctx, "server: starting FIFO server")

	// Retry mechanism for FIFO operations
	maxRetries := 3
	retryDelay := time.Second * 5

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			f.logger.InfoContext(ctx, "server: context cancelled before starting")
			return
		default:
		}

		f.logger.InfoContext(ctx, "server: attempting to start FIFO listener", 
			slog.Int("attempt", attempt), 
			slog.Int("maxRetries", maxRetries))

		if err := f.startFifoListener(ctx); err != nil {
			f.logger.ErrorContext(ctx, "server: FIFO listener failed", 
				slog.Any("error", err),
				slog.Int("attempt", attempt))
			
			if attempt < maxRetries {
				f.logger.InfoContext(ctx, "server: retrying FIFO listener", slog.Duration("delay", retryDelay))
				
				select {
				case <-ctx.Done():
					f.logger.InfoContext(ctx, "server: context cancelled during retry delay")
					return
				case <-time.After(retryDelay):
					continue
				}
			} else {
				f.logger.ErrorContext(ctx, "server: FIFO listener failed after all retries, but continuing to run")
				// Don't return here - keep the server running even if FIFO fails
				break
			}
		} else {
			f.logger.InfoContext(ctx, "server: FIFO listener started successfully")
			break
		}
	}

	// Even if FIFO fails, keep the server running with a fallback mechanism
	f.runFallbackServer(ctx)
}

func (f FifoServer) startFifoListener(ctx context.Context) error {
	ch := make(chan string, 100) // Buffered channel to prevent blocking
	defer close(ch)

	// Start FIFO listener in a separate goroutine
	listenerCtx, listenerCancel := context.WithCancel(ctx)
	defer listenerCancel()

	listenerDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				f.logger.ErrorContext(listenerCtx, "server: recovered from panic in FIFO listener", slog.Any("panic", r))
				listenerDone <- nil // Don't send error for panic recovery
			}
		}()

		err := f.fifo.Listen(listenerCtx, settings.FifoPath, ch)
		listenerDone <- err
	}()

	// Process messages with error recovery
	for {
		select {
		case <-ctx.Done():
			f.logger.InfoContext(ctx, "server: FIFO listener context cancelled")
			return ctx.Err()
		case err := <-listenerDone:
			if err != nil {
				f.logger.ErrorContext(ctx, "server: FIFO listener error", slog.Any("error", err))
				return err
			}
			f.logger.InfoContext(ctx, "server: FIFO listener completed normally")
			return nil
		case msg := <-ch:
			// Handle message with error recovery
			func() {
				defer func() {
					if r := recover(); r != nil {
						f.logger.ErrorContext(ctx, "server: recovered from panic while handling message", 
							slog.Any("panic", r),
							slog.String("message", msg))
					}
				}()
				f.handleWithRetry(ctx, msg)
			}()
		}
	}
}

func (f FifoServer) runFallbackServer(ctx context.Context) {
	f.logger.InfoContext(ctx, "server: running fallback server mode")
	
	// Keep the server alive even if FIFO fails
	ticker := time.NewTicker(time.Minute * 5) // Periodic health check
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.logger.InfoContext(ctx, "server: fallback server context cancelled")
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						f.logger.ErrorContext(ctx, "server: recovered from panic in fallback server", slog.Any("panic", r))
					}
				}()
				
				f.logger.DebugContext(ctx, "server: fallback server health check")
				// Periodic aerospace refresh to keep data fresh
				f.aerospace.SingleFlightRefreshTree()
			}()
		}
	}
}

func (f FifoServer) handleWithRetry(ctx context.Context, msg string) {
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := f.handleSafely(ctx, msg); err != nil {
			f.logger.ErrorContext(ctx, "server: message handling failed", 
				slog.Any("error", err),
				slog.String("message", msg),
				slog.Int("attempt", attempt))
			
			if attempt < maxRetries {
				time.Sleep(time.Millisecond * 100) // Brief delay before retry
				continue
			} else {
				f.logger.ErrorContext(ctx, "server: message handling failed after all retries, skipping message", 
					slog.String("message", msg))
			}
		} else {
			break // Success
		}
	}
}

func (f FifoServer) handleSafely(ctx context.Context, msg string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			f.logger.ErrorContext(ctx, "server: recovered from panic in handleSafely", 
				slog.Any("panic", r),
				slog.String("message", msg))
			err = nil // Convert panic to nil error so we don't retry panics
		}
	}()

	if strings.HasPrefix(msg, "init") {
		f.logger.InfoContext(ctx, "server: handling init message")
		if err := f.config.Init(ctx); err != nil {
			f.logger.ErrorContext(ctx, "server: init failed, but continuing", slog.Any("error", err))
		}
		return nil
	}

	if strings.HasPrefix(msg, events.AerospaceRefresh) {
		f.logger.InfoContext(ctx, "server: handling aerospace refresh")
		
		f.aerospace.SingleFlightRefreshTree()

		if err := f.config.Update(ctx, &args.In{
			Name:  items.AerospaceName,
			Event: events.AerospaceRefresh,
		}); err != nil {
			f.logger.ErrorContext(ctx, "server: aerospace refresh update failed", slog.Any("error", err))
			return err
		}
		return nil
	}

	if strings.HasPrefix(msg, "update") {
		f.logger.InfoContext(ctx, "server: handling update message")
		
		args, err := args.FromEvent(msg)
		if err != nil {
			f.logger.ErrorContext(ctx, "server: could not parse args", slog.Any("error", err))
			return err
		}

		f.logger.InfoContext(ctx, "server: processing update",
			slog.String("name", args.Name),
			slog.String("event", args.Event),
			slog.String("info", args.Info))

		if err := f.config.Update(ctx, args); err != nil {
			f.logger.ErrorContext(ctx, "server: update failed", slog.Any("error", err))
			return err
		}
		return nil
	}

	if strings.HasPrefix(msg, events.WorkspaceChange) {
		f.logger.InfoContext(ctx, "server: handling workspace change")
		
		eventJSON, _ := strings.CutPrefix(msg, events.WorkspaceChange)
		var data events.WorkspaceChangeEventInfo
		
		if err := json.Unmarshal([]byte(eventJSON), &data); err != nil {
			f.logger.ErrorContext(ctx, "server: could not deserialize workspace change data",
				slog.String("message", msg),
				slog.Any("error", err))
			return err
		}

		f.aerospace.SetPrevWorkspaceID(data.Prev)
		f.aerospace.SetFocusedWorkspaceID(data.Focused)

		if err := f.config.Update(ctx, &args.In{
			Name:  items.AerospaceName,
			Event: events.WorkspaceChange,
			Info:  eventJSON,
		}); err != nil {
			f.logger.ErrorContext(ctx, "server: workspace change update failed", slog.Any("error", err))
			return err
		}
		return nil
	}

	f.logger.DebugContext(ctx, "server: unhandled message", slog.String("message", msg))
	return nil
}