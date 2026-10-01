package docker

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// Shell is an interactive shell running on a TTY inside a container. Reads
// return raw terminal output, writes are terminal input. Close ends the session.
type Shell struct {
	client.HijackedResponse
	client      *client.Client
	execID      string
	containerID string
	pid         string // shell PID inside the container
}

func (s *Shell) Read(p []byte) (int, error)  { return s.Reader.Read(p) }
func (s *Shell) Write(p []byte) (int, error) { return s.Conn.Write(p) }

// Close ends the session. Docker leaves an exec running when its connection
// drops, so a shell still alive is hung up, taking its foreground job with it.
func (s *Shell) Close() {
	s.HijackedResponse.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if inspect, err := s.client.ExecInspect(ctx, s.execID, client.ExecInspectOptions{}); err != nil || !inspect.Running {
		return
	}
	kill, err := s.client.ExecCreate(ctx, s.containerID, client.ExecCreateOptions{Cmd: []string{"kill", "-HUP", s.pid}})
	if err == nil {
		_, err = s.client.ExecStart(ctx, kill.ID, client.ExecStartOptions{Detach: true})
	}
	if err != nil {
		slog.Warn("hanging up shell", "exec", s.execID, "error", err)
	}
}

// Resize sets the terminal size of the shell.
func (s *Shell) Resize(ctx context.Context, cols, rows uint) error {
	_, err := s.client.ExecResize(ctx, s.execID, client.ExecResizeOptions{Width: cols, Height: rows})
	return err
}

// Console is attached to the main process of a container, like docker attach.
// Reads return its terminal output, writes are its terminal input. Close
// detaches and leaves the process running.
type Console struct {
	client.HijackedResponse
	client      *client.Client
	containerID string
}

func (c *Console) Read(p []byte) (int, error)  { return c.Reader.Read(p) }
func (c *Console) Write(p []byte) (int, error) { return c.Conn.Write(p) }

// Resize sets the terminal size of the container.
func (c *Console) Resize(ctx context.Context, cols, rows uint) error {
	_, err := c.client.ContainerResize(ctx, c.containerID, client.ContainerResizeOptions{Width: cols, Height: rows})
	return err
}

// Shell starts an interactive shell in the running container name, bash when
// the image has it and sh otherwise. The caller must Close the result.
func (c ContainerServiceImpl) Shell(ctx context.Context, name string) (*Shell, error) {
	exec, err := c.Client.ExecCreate(ctx, name, client.ExecCreateOptions{
		TTY:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		// the first output line is the PID for Close; exec keeps it for bash too
		Cmd: []string{"/bin/sh", "-c", "echo $$; if command -v bash >/dev/null; then exec bash; else exec sh; fi"},
	})
	if err != nil {
		return nil, err
	}
	attach, err := c.Client.ExecAttach(ctx, exec.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return nil, err
	}
	line, err := attach.Reader.ReadString('\n')
	pid := strings.TrimSpace(line)
	if _, perr := strconv.Atoi(pid); err != nil || perr != nil {
		attach.Close()
		return nil, fmt.Errorf("start shell in %q: %s", name, strings.TrimSpace(line))
	}
	return &Shell{HijackedResponse: attach.HijackedResponse, client: c.Client, execID: exec.ID, containerID: name, pid: pid}, nil
}

// Attach attaches to the main process of the running container name. The
// container must have a TTY and open stdin, as the output is otherwise
// multiplexed and there is nothing to type into. The caller must Close the
// result.
func (c ContainerServiceImpl) Attach(ctx context.Context, name string) (*Console, error) {
	inspect, err := c.Client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	if cfg := inspect.Container.Config; !cfg.Tty || !cfg.OpenStdin {
		return nil, fmt.Errorf("attach to %q: %w: needs a TTY and open stdin", name, cerrdefs.ErrFailedPrecondition)
	}
	if !inspect.Container.State.Running {
		return nil, fmt.Errorf("attach to %q: %w: not running", name, cerrdefs.ErrConflict)
	}
	attach, err := c.Client.ContainerAttach(ctx, name, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, err
	}
	return &Console{HijackedResponse: attach.HijackedResponse, client: c.Client, containerID: name}, nil
}
