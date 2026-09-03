package docker

import (
	"context"

	cerrdefs "github.com/containerd/errdefs"
	v1 "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/client"
)

type ImageService interface {
	Ensure(ctx context.Context, name string) (string, error)
	Pull(ctx context.Context, name string) (string, error)
	GetConfig(ctx context.Context, name string) (v1.DockerOCIImageConfig, error)
}

type ImageServiceImpl struct {
	*client.Client
}

func NewImageService(client *client.Client) ImageService {
	return &ImageServiceImpl{client}
}

// Ensure returns the ID of the image, pulling it if it is not present locally.
func (i ImageServiceImpl) Ensure(ctx context.Context, name string) (string, error) {
	image, err := i.Client.ImageInspect(ctx, name)
	if err == nil {
		return image.ID, nil
	}
	if !cerrdefs.IsNotFound(err) {
		return "", err
	}
	return i.Pull(ctx, name)
}

// Pull pulls the image the returns the ID of the image if successful, otherwise it returns an error.
func (i ImageServiceImpl) Pull(ctx context.Context, name string) (string, error) {
	resp, err := i.Client.ImagePull(ctx, name, client.ImagePullOptions{})
	if err != nil {
		return "", err
	}
	if err := resp.Wait(ctx); err != nil {
		return "", err
	}

	image, err := i.Client.ImageInspect(ctx, name)
	if err != nil {
		return "", err
	}
	return image.ID, nil
}
func (i ImageServiceImpl) GetConfig(ctx context.Context, name string) (v1.DockerOCIImageConfig, error) {
	//TODO implement me
	panic("implement me")
}
