# Log Streaming & File Access — Design Notes

How to add websocket console log streaming and user file access (e.g. download/upload a
Minecraft world) on top of the current Docker service layer.

The repo today is a one-shot CLI. Both features need a long-lived HTTP server.

## 1. Structure shift

```
main.go            -> starts http.Server, wires services
docker/log.go      -> LogService  (stream)
docker/files.go    -> FileService (tar in/out)
api/ws_logs.go     -> websocket handler
api/files.go       -> download/upload handlers
```

Container lookup by name/ID stays in `ContainerService`.

## 2. Log stream

`ContainerLogs` gives an `io.ReadCloser`. Not TTY = multiplexed 8-byte-header frames →
demux with `github.com/moby/moby/api/pkg/stdcopy`.

```go
// docker/log.go
type LogService interface {
    Stream(ctx context.Context, id string, tail string, out chan<- LogLine) error
}

type LogLine struct {
    Stream string // "stdout" | "stderr"
    Text   string
}

func (l LogServiceImpl) Stream(ctx context.Context, id, tail string, out chan<- LogLine) error {
    rc, err := l.Client.ContainerLogs(ctx, id, client.ContainerLogsOptions{
        ShowStdout: true, ShowStderr: true,
        Follow:     true,
        Tail:       tail,       // "200" = backlog on connect
        Timestamps: true,
    })
    if err != nil {
        return err
    }
    defer rc.Close()

    outR, outW := io.Pipe()
    errR, errW := io.Pipe()
    go func() {
        _, err := stdcopy.StdCopy(outW, errW, rc)
        outW.CloseWithError(err)
        errW.CloseWithError(err)
    }()

    var wg sync.WaitGroup
    scan := func(r io.Reader, name string) {
        defer wg.Done()
        s := bufio.NewScanner(r)
        s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // MC stacktrace lines get long
        for s.Scan() {
            select {
            case out <- LogLine{Stream: name, Text: s.Text()}:
            case <-ctx.Done():
                return
            }
        }
    }
    wg.Add(2)
    go scan(outR, "stdout")
    go scan(errR, "stderr")
    wg.Wait()
    return ctx.Err()
}
```

Key point: `ContainerLogs` closes when ctx cancels, so the WS handler ctx should be the
request ctx. A container restart kills the stream — reconnect client-side, or re-dial
server-side with `Since` set to the last timestamp seen.

**WS handler** (`github.com/coder/websocket`, better ctx story than gorilla):

```go
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
    c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        OriginPatterns: []string{"panel.example.com"}, // do NOT use InsecureSkipVerify
    })
    if err != nil { return }
    defer c.CloseNow()

    ctx, cancel := context.WithCancel(r.Context())
    defer cancel()

    lines := make(chan docker.LogLine, 256)
    go func() {
        defer close(lines)
        if err := h.logs.Stream(ctx, id, "200", lines); err != nil {
            slog.Debug("log stream ended", "id", id, "error", err)
        }
    }()

    // reader goroutine: detect client close + console input
    go func() {
        for {
            _, msg, err := c.Read(ctx)
            if err != nil { cancel(); return }
            h.console.Send(ctx, id, string(msg)) // see §3
        }
    }()

    for line := range lines {
        wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
        err := wsjson.Write(wctx, c, line)
        wcancel()
        if err != nil { return }
    }
}
```

Backpressure: a buffered chan plus a slow client blocks on `out <-`. Pick a policy —
drop-oldest is right for logs:

```go
select {
case out <- line:
default: // drop, bump a "lines_dropped" counter
}
```

Fan-out: one Docker log stream per container, N websockets. Add a hub
(`map[string]*broadcaster`) with a refcount, otherwise 20 open tabs = 20 Docker API
streams.

## 3. Console input (send MC commands)

Two ways:

**A. stdin attach.** Needs `Config.OpenStdin: true, Tty: false, StdinOnce: false` at
create time — cannot be added later.

```go
res, _ := c.Client.ContainerAttach(ctx, id, client.ContainerAttachOptions{
    Stream: true, Stdin: true,
})
defer res.Close()
res.Conn.Write([]byte(cmd + "\n"))
```

Keep one attach per container with a mutex on the write. `docker attach` stdin competes
with any other attacher.

**B. RCON** (recommended for MC). `itzg/minecraft-server` with `ENABLE_RCON=true`,
`RCON_PASSWORD=...`, port 25575. Use `github.com/gorcon/rcon`. Cleaner: request/response,
command output comes back, no hijacked-conn lifecycle, survives your own server
restarting. Also gives `save-off`/`save-all` for safe backups (§7).

## 4. Files

The Docker archive API is tar streams in both directions.

**Download:**

```go
res, err := cli.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{
    SourcePath: "/data/world",
})
defer res.Content.Close()
// res.Stat has Name, Size, Mode, Mtime
w.Header().Set("Content-Type", "application/x-tar")
w.Header().Set("Content-Disposition", `attachment; filename="world.tar"`)
io.Copy(w, res.Content)   // straight passthrough, no buffering, no temp file
```

Wrap in `gzip.NewWriter(w)` for `.tar.gz`. No Content-Length is available for a directory
— chunked is fine.

Single file: same call with the file path, then `tar.NewReader` → `Next()` once → copy the
body out, so the browser gets the raw file instead of a tar.

**Listing:** no Docker API for it. Options:

- `ContainerStatPath` (HEAD) — exists/size/mode for one path only.
- Exec `ls`/`find` and parse — fragile.
- Tar the directory and read only headers (`hdr, _ := tr.Next()`, skip bodies) — correct,
  but transfers the whole payload.
- **Best: bind mount instead of a named volume**, then list with `os.ReadDir` on the host.
  See §6.

**Upload:**

```go
// build tar from multipart parts, stream, no full buffering
pr, pw := io.Pipe()
go func() {
    tw := tar.NewWriter(pw)
    for {
        part, err := mr.NextPart()
        if err == io.EOF { break }
        name, err := safeJoin(destDir, part.FileName()) // see §5
        ...
        tw.WriteHeader(&tar.Header{
            Name: name, Mode: 0o644, Size: size, ModTime: time.Now(),
        })
        io.Copy(tw, part)
    }
    pw.CloseWithError(tw.Close())
}()
_, err := cli.CopyToContainer(ctx, id, client.CopyToContainerOptions{
    DestinationPath: "/data",
    Content:         pr,
    CopyUIDGID:      false, // true copies host uid/gid — usually wrong
})
```

A tar header needs `Size` up front, but multipart parts are streams with no length. Either
buffer each part to a temp file first, or require the client to send a `.tar`/`.zip` and
repack. The simplest real design: **accept a zip/tar and transcode to tar on the fly** —
the zip central directory carries sizes.

Ownership gotcha: `itzg/minecraft-server` runs as uid 1000. Files land as uid 0 by
default, so the server can't write them. Set `Uid`/`Gid` to 1000 in the tar header, or
`chown` via exec afterwards.

## 5. Security

- **Zip slip.** Never trust a client-supplied path or tar entry name. Reject anything that
  escapes the base after cleaning:

  ```go
  func safeJoin(base, name string) (string, error) {
      p := path.Join(base, path.Clean("/"+name))
      if p != base && !strings.HasPrefix(p, base+"/") {
          return "", fmt.Errorf("unsafe path %q", name)
      }
      return p, nil
  }
  ```

  Docker unpacks the tar as-is; a `../../etc` entry escapes into wherever the volume is
  mounted. Symlinks too — reject `tar.TypeSymlink`/`tar.TypeLink` on upload unless you
  resolve them yourself.
- **Confine to a whitelist root** (e.g. `/data`) per container. The path must never come
  raw from a query string.
- **AuthZ before any of it.** The handler must verify the caller owns that container ID.
  `CopyFromContainer` on an arbitrary ID reads any container's filesystem;
  `CopyToContainer` writes it. That is a full compromise if the ID is user-supplied and
  unchecked.
- **WS origin check** — set `OriginPatterns`, never `InsecureSkipVerify`. Authenticate via
  a token in the first message or a cookie; browsers can't set headers on a WS handshake.
- **Limits:** `http.MaxBytesReader` on upload, a per-part size cap, an entry count cap, and
  a disk quota per volume.
- **Console input is RCE inside the container** (`/op` and friends). Gate it behind a
  permission separate from log-read.

## 6. Recommendation: bind mount, not named volume

`ContainerServiceImpl.Create` currently mounts a named volume. For a panel with file
management, mount a host directory instead:

```go
mount.Mount{Type: mount.TypeBind, Source: "/srv/servers/" + id + "/data", Target: "/data"}
```

File browse/download/upload then become plain `os` calls on the host: directory listing,
ranged downloads, resumable uploads, rename, delete, zipping a subtree. All the things the
archive API makes painful. The Docker archive API stays as the fallback for containers you
didn't provision.

Keep named volumes only if remote Docker hosts are planned, where the host FS isn't yours.

## 7. World download consistency

Copying `/data/world` while the server runs produces torn chunk files. Before the copy,
over RCON:

```
save-off
save-all flush
```

copy, then `save-on`. For upload, the server must be stopped, or it overwrites the new
files on its next save.

## Deps to add

```
github.com/coder/websocket
github.com/gorcon/rcon                 // if RCON route
github.com/moby/moby/api/pkg/stdcopy   // already in module graph
```

## Build order

1. `LogService` + WS handler — self-contained, immediate payoff.
2. Bind-mount switch.
3. File handlers.
4. RCON console.
