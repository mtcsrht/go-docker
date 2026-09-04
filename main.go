package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/moby/moby/api/types/network"
	"github.com/mtcsrht/go-docker/docker"
)

// envFlag collects repeated -e KEY=VALUE flags into a slice.
type envFlag []string

// String renders the collected variables as a comma-separated list.
// It is part of the flag.Value interface.
func (e *envFlag) String() string {
	return strings.Join(*e, ",")
}

// Set validates that value has the form KEY=VALUE and appends it.
// It is part of the flag.Value interface.
func (e *envFlag) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("env must be KEY=VALUE, got %q", value)
	}
	*e = append(*e, value)
	return nil
}

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

	imageArg := flag.String("image", "", "image to use")
	logFormat := flag.String("log-format", "text", "log output format: text or json")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn or error")
	var envVars envFlag
	flag.Var(&envVars, "e", "environment variable KEY=VALUE (repeatable)")
	flag.Parse()

	if err := setupLogger(*logFormat, *logLevel); err != nil {
		fatal("configuring logger", "error", err)
	}

	if *imageArg == "" {
		fatal("-image is required")
	}

	ctx := context.Background()

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

	vol, err := volumeService.Create(ctx)
	if err != nil {
		fatal("creating volume", "error", err)
	}

	imageName := *imageArg
	imageID, err := imageService.Ensure(ctx, imageName)
	if err != nil {
		fatal("ensuring image", "image", imageName, "error", err)
	}
	slog.Info("image ready", "image", imageName, "id", imageID)

	containerName := "container-" + uuid.New().String()
	imageConf, err := imageService.GetConfig(ctx, imageName)
	if err != nil {
		fatal("reading image config", "image", imageName, "error", err)
	}

	mountPath := imageConf.Volumes

	portMap := network.PortMap{}
	for exposedPort := range imageConf.ExposedPorts {
		parsedPort, err := network.ParsePort(exposedPort)
		if err != nil {
			fatal("parsing exposed port", "port", exposedPort, "error", err)
		}
		portMap[parsedPort] = []network.PortBinding{{
			HostIP:   netip.MustParseAddr("127.0.0.1"),
			HostPort: parsedPort.Port(),
		},
		}
	}

	// Could add service name like, container-mc-uuid or something
	containerID, err := containerService.Create(ctx, docker.ContainerSpec{
		Name:         containerName,
		Image:        imageName,
		VolumeName:   vol.Name,
		MountPaths:   mountPath,
		Env:          envVars,
		PortBindings: portMap,
	})
	if err != nil {
		fatal("creating container", "name", containerName, "error", err)
	}
	slog.Info("container created", "id", containerID, "volume", vol.Name)

	containers, err := containerService.Get(ctx, containerName)
	if err != nil {
		fatal("listing containers", "name", containerName, "error", err)
	}
	for _, container := range containers.Items {
		slog.Info("container found", "names", container.Names, "volume", vol.Name)
	}

	err = containerService.Start(ctx, containerID)
	if err != nil {
		fatal("starting container", "id", containerID, "error", err)
	}
	slog.Info("container started", "id", containerID)
}
