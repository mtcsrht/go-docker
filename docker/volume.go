package docker

import (
	"context"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

// VolumeService manages the Docker volumes that hold container data.
type VolumeService interface {
	List(ctx context.Context, name string) (*volume.Volume, error)
	Create(ctx context.Context) (*volume.Volume, error)
	Delete(ctx context.Context, volume volume.Volume) error
}

// VolumeServiceImpl implements VolumeService on top of the Docker API client.
type VolumeServiceImpl struct {
	*client.Client
}

// NewVolumeService returns a VolumeService backed by client.
func NewVolumeService(client *client.Client) VolumeService {
	return &VolumeServiceImpl{Client: client}
}

// List returns the volume named exactly name, or nil if no such volume exists.
// The Docker name filter matches substrings, so the results are rechecked for an
// exact match. A missing volume is not an error.
func (v VolumeServiceImpl) List(ctx context.Context, name string) (*volume.Volume, error) {
	filter := make(client.Filters)
	filter.Add("name", name)

	volumes, err := v.Client.VolumeList(ctx, client.VolumeListOptions{
		Filters: filter,
	})
	if err != nil {
		return nil, err
	}

	// name filter matches substrings, so confirm an exact hit
	for i, found := range volumes.Items {
		if found.Name == name {
			return &volumes.Items[i], nil
		}
	}
	return nil, nil
}

// Create creates a volume on the local driver under a generated "volume-<uuid>"
// name and returns it.
func (v VolumeServiceImpl) Create(ctx context.Context) (*volume.Volume, error) {

	name := "volume-" + uuid.New().String()
	volumeConfig := client.VolumeCreateOptions{
		Name:   name,
		Driver: "local",
	}

	volumeCreate, err := v.Client.VolumeCreate(ctx, volumeConfig)
	if err != nil {
		return nil, err
	}

	return &volumeCreate.Volume, nil
}

// Delete removes the volume, forcing removal even while containers still
// reference it.
func (v VolumeServiceImpl) Delete(ctx context.Context, volume volume.Volume) error {
	_, err := v.Client.VolumeRemove(ctx, volume.Name, client.VolumeRemoveOptions{
		Force: true,
	})
	return err
}
