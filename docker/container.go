package docker

import (
	"context"
	"errors"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// ContainerSpec describes the container to create. Name, Image and VolumeName
// are required; the remaining fields may be left at their zero value.
type ContainerSpec struct {
	// Name is the container name.
	Name string
	// Image is the image reference the container runs.
	Image string
	// VolumeName is the volume mounted at every path in MountPaths. It must
	// already exist.
	VolumeName string
	// MountPaths holds the in-container paths to back with VolumeName, in the
	// shape the image config reports its volumes.
	MountPaths map[string]struct{}
	// Env holds environment variables as KEY=VALUE.
	Env []string
	// PortBindings maps container ports to host bindings.
	PortBindings network.PortMap
}

// ContainerService manages the lifecycle of containers.
type ContainerService interface {
	Create(ctx context.Context, spec ContainerSpec) (string, error)
	Get(ctx context.Context, name string) (*client.ContainerListResult, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	// TODO log
}

// ContainerServiceImpl implements ContainerService using the Docker API client,
// with VolumeService and ImageService for the lookups Create depends on.
type ContainerServiceImpl struct {
	*client.Client
	VolumeService
	ImageService
}

// NewContainerService returns a ContainerService backed by client, vs and is.
func NewContainerService(client *client.Client, vs VolumeService, is ImageService) ContainerService {
	return &ContainerServiceImpl{
		Client:        client,
		VolumeService: vs,
		ImageService:  is,
	}
}

// CreateMountPaths builds one volume mount per path in mountPaths, all backed by
// the single volume volumeName.
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

// Create creates the container described by spec and returns its ID. The volume
// spec.VolumeName must already exist; Create does not create one.
func (c ContainerServiceImpl) Create(ctx context.Context, spec ContainerSpec) (string, error) {
	response, err := c.VolumeService.List(ctx, spec.VolumeName)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("volume not found")
	}

	mounts := CreateMountPaths(spec.MountPaths, spec.VolumeName)

	createResult, err := c.Client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Env: spec.Env,
		},
		HostConfig: &container.HostConfig{
			PortBindings: spec.PortBindings,
			Mounts:       mounts,
		},
		Name:  spec.Name,
		Image: spec.Image,
	})
	if err != nil {
		return "", err
	}
	return createResult.ID, nil
}

// Get returns the containers whose name matches name, including stopped ones.
// The name filter matches substrings, so the result may hold several entries.
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

// Start starts the container identified by name or ID.
func (c ContainerServiceImpl) Start(ctx context.Context, name string) error {
	_, err := c.Client.ContainerStart(ctx, name, client.ContainerStartOptions{})
	return err
}

// Stop stops the container identified by name or ID.
func (c ContainerServiceImpl) Stop(ctx context.Context, name string) error {
	_, err := c.Client.ContainerStop(ctx, name, client.ContainerStopOptions{})
	return err
}

// Remove removes the container identified by name or ID.
func (c ContainerServiceImpl) Remove(ctx context.Context, name string) error {
	_, err := c.Client.ContainerRemove(ctx, name, client.ContainerRemoveOptions{})
	return err
}
