package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type MemorySettings struct {
	// MaxMemory sets the maximum usable memory in MBs for a container
	MaxMemory int64
	// MaxSwap sets the swap in MBs available on top of MaxMemory. Ignored
	// unless MaxMemory is set.
	MaxSwap int64
}

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

	// MemorySettings holds the memory settings of a container
	MemorySettings MemorySettings

	// MilliCPUs set the amount of cores (1500 = 1.5 cores)
	MilliCPUs int64

	// DiskGB sets the amount of GB of storage container has
	DiskGB int64
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
		return "", fmt.Errorf("volume %q not found", spec.VolumeName)
	}

	mounts := CreateMountPaths(spec.MountPaths, spec.VolumeName)

	resources := container.Resources{NanoCPUs: spec.MilliCPUs * 1e6}
	if m := spec.MemorySettings; m.MaxMemory > 0 {
		resources.Memory = m.MaxMemory << 20
		// Docker's MemorySwap is memory+swap combined, not swap alone
		resources.MemorySwap = (m.MaxMemory + m.MaxSwap) << 20
	}

	// size needs overlay2 on xfs with pquota, so only send it when asked for
	var storageOpt map[string]string
	if spec.DiskGB > 0 {
		storageOpt = map[string]string{"size": fmt.Sprintf("%dG", spec.DiskGB)}
	}

	createResult, err := c.Client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Env: spec.Env,
		},
		HostConfig: &container.HostConfig{
			PortBindings:  spec.PortBindings,
			Mounts:        mounts,
			Resources:     resources,
			StorageOpt:    storageOpt,
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		},
		Name:  spec.Name,
		Image: spec.Image,
	})
	if err != nil {
		return "", fmt.Errorf("create container %q: %w", spec.Name, err)
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
