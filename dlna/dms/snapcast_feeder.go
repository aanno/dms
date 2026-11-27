package dms

// PoC (gstreamer-snapcast-rs, docs/dlna.md step 4): dms feeds a Snapcast session per
// item. The renderer's GET with "Accept: audio/x-snapcast-tcp" opens it (snapcast.go),
// and every renderer that gets the same item joins the same session.
//
// Feeding is behind SessionFeeder, so that it can be done in different ways:
//
//   - ProcessFeeder (this file): one helper process per session, which plays the
//     file into a named stream of the Snapcast server and obeys the stream's player
//     commands itself (play, pause, seek from the renderers, via the Snapcast
//     server). gstreamer-snapcast-rs's session-sender is such a helper (GStreamer
//     with snapsink). dms stays pure Go: no cgo, no GStreamer bindings.
//   - Later perhaps ffmpeg, feeding PCM into the Snapcast daemon's socket.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SessionFeeder plays items into Snapcast streams.
type SessionFeeder interface {
	// Open makes sure that a session plays the file at path as stream, and returns
	// once the stream exists on the Snapcast server (renderers can join it). An
	// open session for stream is reused. The session starts paused: the renderer's
	// Play starts it.
	Open(ctx context.Context, stream, path string) error
}

// snapcastStreamName is the Snapcast stream of an item's session: the same item
// always gets the same stream, so every renderer joins the same session.
func snapcastStreamName(path string) string {
	sum := sha1.Sum([]byte(path))
	return "dms-" + hex.EncodeToString(sum[:6])
}

// ProcessFeeder runs Command with the arguments "--paused <file> <stream>" appended,
// once per session. The helper prints a line starting with "Session ready" once the
// stream exists, and exits by itself when the session is idle (e.g.
// session-sender's --idle-exit).
type ProcessFeeder struct {
	// The helper and its fixed arguments, e.g.
	// ["session-sender", "--mode", "auto", "--idle-exit", "600"].
	Command []string
	// How long Open waits for "Session ready".
	ReadyTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*feederSession
}

type feederSession struct {
	ready chan struct{} // closed when ready (or failed: see err)
	err   error
}

// Open implements SessionFeeder.
func (f *ProcessFeeder) Open(ctx context.Context, stream, path string) error {
	f.mu.Lock()
	if f.sessions == nil {
		f.sessions = make(map[string]*feederSession)
	}
	s, running := f.sessions[stream]
	if !running {
		s = &feederSession{ready: make(chan struct{})}
		f.sessions[stream] = s
		go f.run(stream, path, s)
	}
	f.mu.Unlock()

	timeout := f.ReadyTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	select {
	case <-s.ready:
		return s.err
	case <-time.After(timeout):
		return fmt.Errorf("session %s: not ready after %v", stream, timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run starts the helper for stream and waits for it to end.
func (f *ProcessFeeder) run(stream, path string, s *feederSession) {
	var readyOnce sync.Once
	markReady := func(err error) {
		readyOnce.Do(func() {
			s.err = err
			close(s.ready)
		})
	}
	defer func() {
		f.mu.Lock()
		if f.sessions[stream] == s {
			delete(f.sessions, stream)
		}
		f.mu.Unlock()
	}()

	args := append(append([]string{}, f.Command[1:]...), "--paused", path, stream)
	cmd := exec.Command(f.Command[0], args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		markReady(err)
		return
	}
	cmd.Stderr = cmd.Stdout // one stream to log
	if err := cmd.Start(); err != nil {
		markReady(fmt.Errorf("session feeder %s: %w", f.Command[0], err))
		return
	}
	slog.Info("snapcast session opening", "stream", stream, "path", path, "pid", cmd.Process.Pid)
	logHelper(stdout, stream, func() { markReady(nil) })
	err = cmd.Wait()
	markReady(fmt.Errorf("session %s: the feeder ended before it was ready (%v)", stream, err))
	slog.Info("snapcast session closed", "stream", stream, "exit", err)
}

// logHelper logs the helper's output until it closes, and calls ready on the
// "Session ready" line.
func logHelper(r io.Reader, stream string, ready func()) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Info("snapcast session", "stream", stream, "feeder", line)
		if strings.HasPrefix(line, "Session ready") {
			ready()
		}
	}
}
