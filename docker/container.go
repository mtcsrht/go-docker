package docker

import (
	"context"
	"errors"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type ContainerService interface {
	Create(ctx context.Context, name string, image string, volumeName string, mountPaths map[string]struct{}, env []string, portBindings network.PortMap) (string, error)
	Get(ctx context.Context, name string) (*client.ContainerListResult, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	// TODO log
}

type ContainerServiceImpl struct {
	*client.Client
	VolumeService
	ImageService
}

func NewContainerService(client *client.Client, vs VolumeService, is ImageService) ContainerService {
	return &ContainerServiceImpl{
		Client:        client,
		VolumeService: vs,
		ImageService:  is,
	}
}

func CreateMountPaths(mountPaths map[string]struct{}, volumeName string) []mount.Mount {
	mounts := make([]mount.Mount, 0, len(mountPaths))
	for mountPath := range mountPaths {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: mountPath,
		})
	}
	return mounts
}

func (c ContainerServiceImpl) Create(ctx context.Context, name string, image string, volumeName string, mountPaths map[string]struct{}, env []string, portBindings network.PortMap) (string, error) {
	response, err := c.VolumeService.List(ctx, volumeName)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("volume not found")
	}

	mounts := CreateMountPaths(mountPaths, volumeName)

	createResult, err := c.Client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Env: env,
		},
		HostConfig: &container.HostConfig{
			PortBindings: portBindings,
			Mounts:       mounts,
		},
		Name:  name,
		Image: image,
	})
	if err != nil {
		return "", err
	}
	return createResult.ID, nil
}

func (c ContainerServiceImpl) Get(ctx context.Context, name string) (*client.ContainerListResult, error) {
	var filter = make(client.Filters)
	filter.Add("name", name)
	result, err := c.Client.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c ContainerServiceImpl) Start(ctx context.Context, name string) error {
	_, err := c.Client.ContainerStart(ctx, name, client.ContainerStartOptions{})
	return err
}

func (c ContainerServiceImpl) Stop(ctx context.Context, name string) error {
	//TODO implement me
	panic("implement me")
}

func (c ContainerServiceImpl) Remove(ctx context.Context, name string) error {
	//TODO implement me
	panic("implement me")
}
