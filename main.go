package main

import (
	"context"
	"fmt"
	"go-docker/docker"
	"log"
	"net/netip"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/moby/moby/api/types/network"
)

func main() {
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
	imageName := "mongo:latest"
	imageID, err := imageService.Ensure(ctx, imageName)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("image %s ready\n", imageID)

	var envVars []string
	envVars = append(envVars, "POSTGRES_USER=mc")
	envVars = append(envVars, "POSTGRES_PASSWORD=mcFasz")
	envVars = append(envVars, "POSTGRES_DB=minecraft")

	containerPort, err := network.ParsePort("5432/tcp")
	if err != nil {
		log.Fatal(err)
	}

	porBindings := make([]network.PortBinding, 0)
	porBindings = append(porBindings, network.PortBinding{
		HostIP:   netip.MustParseAddr("127.0.0.1"),
		HostPort: "5432",
	})

	portMap := network.PortMap{
		containerPort: porBindings,
	}

	containerName := "container-" + uuid.New().String()
	imageConf, err := imageService.GetConfig(ctx, imageName)
	if err != nil {
		log.Fatal(err)
	}

	mountPath := imageConf.Volumes

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
}
