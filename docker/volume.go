package docker

import (
	"context"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type VolumeService interface {
	List(ctx context.Context, name string) (*volume.Volume, error)
	Create(ctx context.Context) (*volume.Volume, error)
	Delete(ctx context.Context, volume volume.Volume) error
}

type VolumeServiceImpl struct {
	*client.Client
}

func NewVolumeService(client *client.Client) VolumeService {
	return &VolumeServiceImpl{Client: client}
}

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

func (v VolumeServiceImpl) Delete(ctx context.Context, volume volume.Volume) error {
	_, err := v.Client.VolumeRemove(ctx, volume.Name, client.VolumeRemoveOptions{
		Force: true,
	})
	return err
}
