package fifo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"
)

const Separator = '¬'

type Reader struct {
	logger *slog.Logger
}

func NewFifoReader(logger *slog.Logger) *Reader {
	return &Reader{
		logger,
	}
}

func (f *Reader) makeSureFifoExists(path string) error {
	stat, err := os.Stat(path)
	if err == nil {
		if stat.Mode()&os.ModeNamedPipe == 0 {
			f.logger.WarnContext(
				context.Background(),
				"fifo: path exists but is a regular file, not a named pipe. Removing it to create a FIFO.",
				slog.String("path", path),
			)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("fifo: could not remove existing file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("fifo: could not stat file: %w", err)
	}

	if err := syscall.Mkfifo(path, 0640); err != nil {
		return fmt.Errorf("fifo: could not create fifo file: %w", err)
	}
	f.logger.InfoContext(context.Background(), "fifo: successfully created fifo file", slog.String("path", path))
	return nil
}

func (f *Reader) Start(path string) error {
	if err := f.makeSureFifoExists(path); err != nil {
		return fmt.Errorf("fifo: error creating file: %w", err)
	}
	return nil
}

func (f *Reader) Listen(
	ctx context.Context,
	path string,
	ch chan<- string,
) error {
	defer func() {
		if r := recover(); r != nil {
			f.logger.ErrorContext(ctx, "fifo: recovered from panic in Listen", slog.Any("panic", r))
		}
	}()

	maxRetries := 3
	retryDelay := time.Second * 2

	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			f.logger.InfoContext(ctx, "fifo: context cancelled before retry")
			return ctx.Err()
		default:
		}

		f.logger.InfoContext(ctx, "fifo: attempting to open FIFO",
			slog.String("path", path),
			slog.Int("attempt", attempt))

		err := f.listenAttempt(ctx, path, ch)

		if err == nil {
			f.logger.InfoContext(ctx, "fifo: listen completed successfully")
			return nil
		}

		// Handle specific error types
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			f.logger.InfoContext(ctx, "fifo: context cancelled/timeout during listen")
			return err
		}

		f.logger.ErrorContext(ctx, "fifo: listen attempt failed",
			slog.Any("error", err),
			slog.Int("attempt", attempt),
			slog.Int("maxRetries", maxRetries))

		if attempt < maxRetries {
			f.logger.InfoContext(ctx, "fifo: retrying listen", slog.Duration("delay", retryDelay))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
				// Recreate FIFO before retry
				if recreateErr := f.makeSureFifoExists(path); recreateErr != nil {
					f.logger.ErrorContext(ctx, "fifo: failed to recreate FIFO", slog.Any("error", recreateErr))
				}
				continue
			}
		}
	}

	f.logger.ErrorContext(ctx, "fifo: all listen attempts failed, continuing anyway")
	return fmt.Errorf("fifo: failed to establish stable connection after %d attempts", maxRetries)
}

func (f *Reader) listenAttempt(
	ctx context.Context,
	path string,
	ch chan<- string,
) error {
	defer func() {
		if r := recover(); r != nil {
			f.logger.ErrorContext(ctx, "fifo: recovered from panic in listenAttempt", slog.Any("panic", r))
		}
	}()

	pipe, err := f.openFifoSafely(ctx, path)
	if err != nil {
		return fmt.Errorf("fifo: error opening for reading: %w", err)
	}

	defer func() {
		if closeErr := pipe.Close(); closeErr != nil {
			f.logger.ErrorContext(ctx, "fifo: error closing pipe", slog.Any("error", closeErr))
		}
	}()

	reader := bufio.NewReader(pipe)
	internalCh := make(chan []byte, 100) // Buffered channel
	readerDone := make(chan error, 1)
	continueReading := true

	defer close(internalCh)

	// Reader goroutine with error recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				f.logger.ErrorContext(ctx, "fifo: recovered from panic in reader goroutine", slog.Any("panic", r))
				readerDone <- fmt.Errorf("reader panic: %v", r)
			}
		}()

		for continueReading {
			select {
			case <-ctx.Done():
				readerDone <- ctx.Err()
				return
			default:
			}

			line, readErr := reader.ReadBytes(Separator)

			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					f.logger.InfoContext(ctx, "fifo: received EOF, stopping reader")
					readerDone <- readErr
					return
				}

				if errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EWOULDBLOCK) {
					f.logger.DebugContext(ctx, "fifo: no data, continuing")
					time.Sleep(100 * time.Millisecond)
					continue
				}

				f.logger.ErrorContext(ctx, "fifo: read error", slog.Any("error", readErr))
				readerDone <- readErr
				return
			}

			if continueReading {
				select {
				case internalCh <- line:
				case <-ctx.Done():
					readerDone <- ctx.Err()
					return
				default:
					f.logger.WarnContext(ctx, "fifo: channel full, dropping message")
				}
			}
		}

		readerDone <- nil
	}()

	// Message processing loop
	for {
		select {
		case <-ctx.Done():
			f.logger.InfoContext(ctx, "fifo: context cancelled")
			continueReading = false

			// Clean shutdown
			f.ensureCloseWithTimeout(path, time.Second*5)
			return ctx.Err()

		case err := <-readerDone:
			f.logger.InfoContext(ctx, "fifo: reader goroutine finished", slog.Any("error", err))
			continueReading = false

			f.ensureCloseWithTimeout(path, time.Second*5)
			return err

		case data := <-internalCh:
			func() {
				defer func() {
					if r := recover(); r != nil {
						f.logger.ErrorContext(ctx, "fifo: recovered from panic while processing message", slog.Any("panic", r))
					}
				}()

				nline := string(data)
				nline = strings.TrimRight(nline, string(Separator))
				nline = strings.TrimLeft(nline, "\n")
				nline = strings.TrimSpace(nline)

				if nline != "" {
					select {
					case ch <- nline:
					case <-ctx.Done():
						return
					default:
						f.logger.WarnContext(ctx, "fifo: output channel full, dropping message", slog.String("message", nline))
					}
				}
			}()
		}
	}
}

func (f *Reader) openFifoSafely(ctx context.Context, path string) (*os.File, error) {
	// Check if FIFO exists before opening
	if stat, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			f.logger.InfoContext(ctx, "fifo: FIFO doesn't exist, creating", slog.String("path", path))
			if err := f.makeSureFifoExists(path); err != nil {
				return nil, fmt.Errorf("fifo: could not create FIFO: %w", err)
			}
		} else {
			return nil, fmt.Errorf("fifo: error checking FIFO: %w", err)
		}
	} else if stat.Mode()&os.ModeNamedPipe == 0 {
		f.logger.WarnContext(ctx, "fifo: path exists but is not a named pipe, recreating", slog.String("path", path))
		if err := os.Remove(path); err != nil {
			f.logger.ErrorContext(ctx, "fifo: could not remove non-FIFO file", slog.Any("error", err))
		}
		if err := f.makeSureFifoExists(path); err != nil {
			return nil, fmt.Errorf("fifo: could not recreate FIFO: %w", err)
		}
	}

	// Open with timeout context
	openCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	openDone := make(chan struct{})
	var pipe *os.File
	var openErr error

	go func() {
		defer close(openDone)
		pipe, openErr = os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, os.ModeNamedPipe)
	}()

	select {
	case <-openCtx.Done():
		return nil, fmt.Errorf("fifo: timeout opening FIFO: %w", openCtx.Err())
	case <-openDone:
		if openErr != nil {
			return nil, fmt.Errorf("fifo: error opening FIFO: %w", openErr)
		}
		return pipe, nil
	}
}

func (f *Reader) ensureCloseWithTimeout(path string, timeout time.Duration) {
	done := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic during cleanup: %v", r)
			}
		}()

		done <- f.ensureClose(path)
	}()

	select {
	case err := <-done:
		if err != nil {
			f.logger.ErrorContext(context.Background(), "fifo: error during cleanup", slog.Any("error", err))
		}
	case <-time.After(timeout):
		f.logger.WarnContext(context.Background(), "fifo: cleanup timeout", slog.Duration("timeout", timeout))
	}
}

func (f *Reader) ensureClose(path string) error {
	defer func() {
		if r := recover(); r != nil {
			f.logger.ErrorContext(context.Background(), "fifo: recovered from panic in ensureClose", slog.Any("panic", r))
		}
	}()

	// Try to remove the FIFO file
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("fifo: could not remove fifo: %w", err)
	}

	return nil
}