package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/mtcsrht/go-docker/docker"
	"log"
	"net/netip"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/moby/moby/api/types/network"
)

type envFlag []string

func (e *envFlag) String() string {
	return strings.Join(*e, ",")
}

func (e *envFlag) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("env must be KEY=VALUE, got %q", value)
	}
	*e = append(*e, value)
	return nil
}

func main() {

	imageArg := flag.String("image", "", "image to use")
	var envVars envFlag
	flag.Var(&envVars, "e", "environment variable KEY=VALUE (repeatable)")
	flag.Parse()

	if *imageArg == "" {
		log.Fatal("-image is required")
	}

	ctx := context.Background()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	dockerClient, err := docker.NewDockerClient()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := dockerClient.Close(); err != nil {
			log.Fatal("closing docker client", "error", err)
		}
	}()

	volumeService := docker.NewVolumeService(dockerClient)
	imageService := docker.NewImageService(dockerClient)
	containerService := docker.NewContainerService(dockerClient, volumeService, imageService)

	vol, err := volumeService.Create(ctx)
	if err != nil {
		log.Fatal(err)
	}

	imageName := *imageArg
	imageID, err := imageService.Ensure(ctx, imageName)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("image %s ready\n", imageID)

	containerName := "container-" + uuid.New().String()
	imageConf, err := imageService.GetConfig(ctx, imageName)
	if err != nil {
		log.Fatal(err)
	}

	mountPath := imageConf.Volumes

	portMap := network.PortMap{}
	for exposedPort := range imageConf.ExposedPorts {
		parsedPort, err := network.ParsePort(exposedPort)
		if err != nil {
			log.Fatal(err)
		}
		portMap[parsedPort] = []network.PortBinding{{
			HostIP:   netip.MustParseAddr("127.0.0.1"),
			HostPort: parsedPort.Port(),
		},
		}
	}

	containerID, err := containerService.Create(ctx, containerName, imageName, vol.Name, mountPath, envVars, portMap) // Could add service name like, container-mc-uuid or something
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("container %s created on volume %s\n", containerID, vol.Name)

	containers, err := containerService.Get(ctx, containerName)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Looking for container")
	for _, container := range containers.Items {
		fmt.Printf("container %s found on volume %s\n", container.Names, vol.Name)
	}

	err = containerService.Start(ctx, containerID)
	if err != nil {
		log.Fatal(err)
	}
}
