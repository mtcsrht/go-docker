package api

// templates are the presets served by POST /templates/{name}/containers. To add
// a game server, add an entry: the image, the env it needs and its resources.
var templates = map[string]createRequest{
	// MEMORY is the JVM heap; it is kept below MemoryMB to leave room for
	// the JVM's own overhead.
	"minecraft-small": {
		Image:     "itzg/minecraft-server",
		Env:       []string{"EULA=TRUE", "MEMORY=1536M"},
		MemoryMB:  2048,
		MilliCPUs: 1000,
		TTY:       true,
		StdinOpen: true,
	},
	"minecraft-medium": {
		Image:     "itzg/minecraft-server",
		Env:       []string{"EULA=TRUE", "MEMORY=3G"},
		MemoryMB:  4096,
		MilliCPUs: 2000,
		TTY:       true,
		StdinOpen: true,
	},
	"minecraft-large": {
		Image:     "itzg/minecraft-server",
		Env:       []string{"EULA=TRUE", "MEMORY=6G"},
		MemoryMB:  8192,
		MilliCPUs: 4000,
		TTY:       true,
		StdinOpen: true,
	},
}
