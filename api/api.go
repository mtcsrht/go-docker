package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/network"
	"github.com/mtcsrht/go-docker/docker"
)

// ErrorResponse is the body of every error the API returns.
type ErrorResponse struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	Description string `json:"description"`
}

// Server exposes the docker services over HTTP.
type Server struct {
	containers docker.ContainerService
	volumes    docker.VolumeService
	images     docker.ImageService
}

// NewHandler returns the HTTP handler serving the REST and websocket routes.
func NewHandler(cs docker.ContainerService, vs docker.VolumeService, is docker.ImageService) http.Handler {
	s := &Server{containers: cs, volumes: vs, images: is}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /containers", s.create)
	mux.HandleFunc("GET /containers", s.list)
	mux.HandleFunc("POST /containers/{id}/start", s.start)
	mux.HandleFunc("POST /containers/{id}/stop", s.stop)
	mux.HandleFunc("DELETE /containers/{id}", s.remove)
	mux.HandleFunc("GET /containers/{id}/logs", s.logs)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	return mux
}

// indexHTML is a static test page for the API, served at /.
//
//go:embed index.html
var indexHTML []byte

// createRequest is the body of POST /containers. Zero resource fields mean
// no limit.
type createRequest struct {
	Image     string   `json:"image"`
	Env       []string `json:"env"`
	MemoryMB  int64    `json:"memoryMB"`
	SwapMB    int64    `json:"swapMB"`
	MilliCPUs int64    `json:"milliCPUs"`
	DiskGB    int64    `json:"diskGB"`
}

// validate reports the first invalid field of r.
func (r createRequest) validate() error {
	if r.Image == "" {
		return errors.New("image is required")
	}
	for _, e := range r.Env {
		if !strings.Contains(e, "=") {
			return fmt.Errorf("env must be KEY=VALUE, got %q", e)
		}
	}
	if r.MemoryMB < 0 || r.SwapMB < 0 || r.MilliCPUs < 0 || r.DiskGB < 0 {
		return errors.New("resource limits must not be negative")
	}
	return nil
}

type createResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Volume string `json:"volume"`
}

// create ensures the image, creates a fresh volume and creates a container on
// it, mounting the volume at every path the image declares and publishing every
// exposed port on 127.0.0.1.
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Request body must be valid JSON: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx := r.Context()
	if _, err := s.images.Ensure(ctx, req.Image); err != nil {
		writeDockerError(w, "ensuring image", err)
		return
	}
	imageConf, err := s.images.GetConfig(ctx, req.Image)
	if err != nil {
		writeDockerError(w, "reading image config", err)
		return
	}

	portMap := network.PortMap{}
	for exposedPort := range imageConf.ExposedPorts {
		port, err := network.ParsePort(exposedPort)
		if err != nil {
			writeDockerError(w, "parsing exposed port", err)
			return
		}
		portMap[port] = []network.PortBinding{{
			HostIP:   netip.MustParseAddr("127.0.0.1"),
			HostPort: port.Port(),
		}}
	}

	vol, err := s.volumes.Create(ctx)
	if err != nil {
		writeDockerError(w, "creating volume", err)
		return
	}

	name := "container-" + uuid.New().String()
	id, err := s.containers.Create(ctx, docker.ContainerSpec{
		Name:           name,
		Image:          req.Image,
		VolumeName:     vol.Name,
		MountPaths:     imageConf.Volumes,
		Env:            req.Env,
		PortBindings:   portMap,
		MemorySettings: docker.MemorySettings{MaxMemory: req.MemoryMB, MaxSwap: req.SwapMB},
		MilliCPUs:      req.MilliCPUs,
		DiskGB:         req.DiskGB,
	})
	if err != nil {
		// the volume was made for this container only, so don't leak it
		if derr := s.volumes.Delete(context.WithoutCancel(ctx), *vol); derr != nil {
			slog.Error("deleting orphaned volume", "volume", vol.Name, "error", derr)
		}
		writeDockerError(w, "creating container", err)
		return
	}
	slog.Info("container created", "id", id, "name", name, "volume", vol.Name)
	writeJSON(w, http.StatusCreated, createResponse{ID: id, Name: name, Volume: vol.Name})
}

// list returns the containers whose name contains the name query parameter.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	result, err := s.containers.Get(r.Context(), r.URL.Query().Get("name"))
	if err != nil {
		writeDockerError(w, "listing containers", err)
		return
	}
	writeJSON(w, http.StatusOK, result.Items)
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, "starting container", s.containers.Start)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, "stopping container", s.containers.Stop)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, "removing container", s.containers.Remove)
}

// lifecycle runs op on the {id} path value and answers 204 on success.
func (s *Server) lifecycle(w http.ResponseWriter, r *http.Request, action string, op func(context.Context, string) error) {
	id := r.PathValue("id")
	if err := op(r.Context(), id); err != nil {
		writeDockerError(w, action, err)
		return
	}
	slog.Info(action, "id", id)
	w.WriteHeader(http.StatusNoContent)
}

// logFrame is one websocket message of the log stream. Data is a raw chunk of
// output and need not end on a line boundary.
type logFrame struct {
	Stream string `json:"stream"`
	Data   string `json:"data"`
}

// wsWriter sends every Write as a logFrame tagged with stream.
type wsWriter struct {
	ctx    context.Context
	conn   *websocket.Conn
	stream string
}

func (w wsWriter) Write(p []byte) (int, error) {
	// a stalled client must not pin the docker log stream forever
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, w.conn, logFrame{Stream: w.stream, Data: string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// logs upgrades to a websocket and follows the container's logs, starting from
// the last tail lines (query parameter, default 100), until the client
// disconnects or the container stops.
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	// nil options keep the same-origin check for browser clients
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the error response
	}
	defer conn.CloseNow()

	// CloseRead cancels ctx once the client goes away
	ctx := conn.CloseRead(r.Context())
	err = s.containers.StreamLogs(ctx, id, tail,
		wsWriter{ctx, conn, "stdout"}, wsWriter{ctx, conn, "stderr"})
	switch {
	case ctx.Err() != nil:
	case cerrdefs.IsNotFound(err):
		conn.Close(websocket.StatusPolicyViolation, "container not found")
	case err != nil:
		slog.Error("streaming logs", "id", id, "error", err)
		conn.Close(websocket.StatusInternalError, "log stream failed")
	default:
		conn.Close(websocket.StatusNormalClosure, "container stopped")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message, description string) {
	writeJSON(w, status, ErrorResponse{Code: status, Message: message, Description: description})
}

// writeDockerError maps err from the docker layer to an ErrorResponse. Server
// errors are logged and their details kept out of the response.
func writeDockerError(w http.ResponseWriter, action string, err error) {
	switch {
	case cerrdefs.IsNotFound(err):
		writeError(w, http.StatusNotFound, "entity_not_found", err.Error())
	case cerrdefs.IsConflict(err):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case cerrdefs.IsInvalidArgument(err):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		slog.Error(action, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_server_error",
			"An unexpected error occurred. Please try again later.")
	}
}
