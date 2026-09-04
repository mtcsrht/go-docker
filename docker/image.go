package docker

import (
	"context"
	"log/slog"

	cerrdefs "github.com/containerd/errdefs"
	v1 "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/client"
)

// ImageService resolves, pulls and inspects images.
type ImageService interface {
	Ensure(ctx context.Context, name string) (string, error)
	Pull(ctx context.Context, name string) (string, error)
	GetConfig(ctx context.Context, name string) (*v1.DockerOCIImageConfig, error)
}

// ImageServiceImpl implements ImageService on top of the Docker API client.
type ImageServiceImpl struct {
	*client.Client
}

// NewImageService returns an ImageService backed by client.
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
// The start and the completion of the download are logged.
func (i ImageServiceImpl) Pull(ctx context.Context, name string) (string, error) {
	slog.Info("pulling image", "image", name)

	resp, err := i.Client.ImagePull(ctx, name, client.ImagePullOptions{})
	if err != nil {
		return "", err
	}
	if err := resp.Wait(ctx); err != nil {
		return "", err
	}
	slog.Info("pulled image", "image", name)

	image, err := i.Client.ImageInspect(ctx, name)
	if err != nil {
		return "", err
	}
	return image.ID, nil
}

// GetConfig returns the image's embedded OCI config, which records what the image
// declares at build time: entrypoint, command, exposed ports, env and volumes.
func (i ImageServiceImpl) GetConfig(ctx context.Context, name string) (*v1.DockerOCIImageConfig, error) {
	img, err := i.Client.ImageInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	return img.Config, err
}
