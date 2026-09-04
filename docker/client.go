package docker

import "github.com/moby/moby/client"

// NewDockerClient returns a Docker API client configured from the environment
// (DOCKER_HOST, DOCKER_API_VERSION, and the TLS variables).
func NewDockerClient() (*client.Client, error) {
	return client.New(client.FromEnv)
}
