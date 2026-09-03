package docker

import "github.com/moby/moby/client"

func NewDockerClient() (*client.Client, error) {
	return client.New(client.FromEnv)
}
