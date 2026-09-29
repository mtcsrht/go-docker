package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mtcsrht/go-docker/api"
	"github.com/mtcsrht/go-docker/docker"
)

// fatal logs msg with the given attributes and exits with a failure status.
// Like log.Fatal, it does not run deferred functions.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// setupLogger installs the process-wide slog handler. format selects the output
// encoding, "text" or "json"; level is a slog level name such as debug or info.
func setupLogger(format string, level string) error {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return fmt.Errorf("log level %q: %w", level, err)
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		return fmt.Errorf("log format %q: want text or json", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	logFormat := flag.String("log-format", "text", "log output format: text or json")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn or error")
	flag.Parse()

	if err := setupLogger(*logFormat, *logLevel); err != nil {
		fatal("configuring logger", "error", err)
	}

	err := godotenv.Load()
	if err != nil {
		fatal("loading .env file", "error", err)
	}

	dockerClient, err := docker.NewDockerClient()
	if err != nil {
		fatal("creating docker client", "error", err)
	}
	defer func() {
		if err := dockerClient.Close(); err != nil {
			fatal("closing docker client", "error", err)
		}
	}()

	volumeService := docker.NewVolumeService(dockerClient)
	imageService := docker.NewImageService(dockerClient)
	containerService := docker.NewContainerService(dockerClient, volumeService, imageService)

	// cancelled on shutdown, which also ends open log streams
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.NewHandler(containerService, volumeService, imageService),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", *addr)

	select {
	case err := <-serveErr:
		fatal("serving http", "error", err)
	case <-ctx.Done():
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("shutting down", "error", err)
	}
	slog.Info("stopped")
}
