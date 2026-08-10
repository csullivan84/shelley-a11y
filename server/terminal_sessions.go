package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"shelley.exe.dev/dtach"
	"shelley.exe.dev/unixsocket"
)

// TerminalSession is the on-disk + in-memory record of a persistent terminal.
// The owning process is a `shelley dtach new` child detached via setsid so
// it survives the parent shelley exiting; it terminates when the user's
// command exits or the session is killed.
type TerminalSession struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Cwd       string    `json:"cwd"`
	Socket    string    `json:"socket"`
	LogFile   string    `json:"log_file"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
}

// SpawnerFunc starts a dtach server hosting `cmd` on the given socket. The
// implementation must not return until the socket is ready to accept
// connections. The default spawns an out-of-process `shelley dtach serve`
// child so sessions outlive the parent shelley. Tests can replace it to run
// in-process.
type SpawnerFunc func(socket, logFile, cwd, command string, cols, rows uint16, extraEnv []string) (pid int, err error)

// TerminalSessions tracks persistent dtach sessions on disk.
type TerminalSessions struct {
	dir      string // root directory holding per-session files
	exe      string // path to the shelley executable (for re-exec)
	logger   *slog.Logger
	spawner  SpawnerFunc
	mu       sync.Mutex
	sessions map[string]*TerminalSession
	// attachMu serializes attachOrSpawn for the duration of socket-stat /
	// spawn so concurrent reconnects for the same id don't double-spawn.
	attachMu sync.Mutex
}

// NewTerminalSessions opens (or creates) a sessions directory and reaps any
// stale records left over from previous runs.
func NewTerminalSessions(dir string, logger *slog.Logger) (*TerminalSessions, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("terminals: mkdir %s: %w", dir, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("terminals: locate shelley executable: %w", err)
	}
	ts := &TerminalSessions{
		dir:      dir,
		exe:      exe,
		logger:   logger,
		sessions: make(map[string]*TerminalSession),
	}
	ts.spawner = ts.spawnSubprocess
	ts.scan()
	return ts, nil
}

// SetSpawner overrides the spawn strategy (intended for tests).
func (t *TerminalSessions) SetSpawner(s SpawnerFunc) { t.spawner = s }

// scan loads sessions from disk, dropping any whose dtach socket is dead.
func (t *TerminalSessions) scan() {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-len(".json")]
		data, err := os.ReadFile(filepath.Join(t.dir, name))
		if err != nil {
			continue
		}
		var s TerminalSession
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if !t.socketAlive(s.Socket) {
			t.removeFiles(id)
			continue
		}
		t.sessions[id] = &s
	}
}

func (t *TerminalSessions) socketAlive(path string) bool {
	path, err := unixsocket.Path(path)
	if err != nil {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (t *TerminalSessions) removeFiles(id string) {
	os.Remove(filepath.Join(t.dir, id+".json"))
	socket := filepath.Join(t.dir, id+".sock")
	if resolved, err := unixsocket.Path(socket); err == nil {
		os.Remove(resolved)
	}
	os.Remove(filepath.Join(t.dir, id+".log"))
}

// List returns a snapshot of known live sessions, oldest first.
func (t *TerminalSessions) List() []*TerminalSession {
	t.mu.Lock()
	out := make([]*TerminalSession, 0, len(t.sessions))
	for _, s := range t.sessions {
		out = append(out, s)
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Get returns a session by ID, or nil.
func (t *TerminalSessions) Get(id string) *TerminalSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[id]
}

// Spawn launches a new dtach-backed session, immediately attaches to it, and
// returns the session record together with the attached client. Doing the
// attach inline closes the race where a fast-exiting command tears down the
// socket before any external attach can succeed.
func (t *TerminalSessions) Spawn(command, cwd string, cols, rows uint16, extraEnv []string) (*TerminalSession, *dtach.Client, error) {
	if command == "" {
		return nil, nil, errors.New("terminals: empty command")
	}
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		} else {
			cwd = "/"
		}
	}
	id, err := newTerminalID()
	if err != nil {
		return nil, nil, err
	}
	socket := filepath.Join(t.dir, id+".sock")
	logFile := filepath.Join(t.dir, id+".log")

	// SHELLEY_TERMINAL_ID identifies this dtach session. It's stable across
	// reattaches because the id is the on-disk session id.
	env := append([]string(nil), extraEnv...)
	env = append(env, "SHELLEY_TERMINAL_ID="+id)

	pid, err := t.spawner(socket, logFile, cwd, command, cols, rows, env)
	if err != nil {
		return nil, nil, err
	}

	// Attach immediately to pin the session open: while at least one client
	// is connected, Serve will not tear down even if the command exits quickly.
	dc, attachErr := attachWithRetry(socket, 3*time.Second)
	if attachErr != nil {
		return nil, nil, fmt.Errorf("terminals: attach freshly spawned session: %w", attachErr)
	}

	sess := &TerminalSession{
		ID:        id,
		Command:   command,
		Cwd:       cwd,
		Socket:    socket,
		LogFile:   logFile,
		PID:       pid,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		dc.Close()
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(t.dir, id+".json"), data, 0o600); err != nil {
		dc.Close()
		return nil, nil, err
	}

	t.mu.Lock()
	t.sessions[id] = sess
	t.mu.Unlock()
	t.logger.Info("spawned persistent terminal", "id", id, "command", command, "cwd", cwd, "pid", sess.PID)
	return sess, dc, nil
}

// attachWithRetry repeatedly tries to dial the dtach socket for up to the
// given deadline. Sub-process spawns can race ahead of accept(), so a brief
// retry loop avoids spurious failures.
func attachWithRetry(socket string, max time.Duration) (*dtach.Client, error) {
	deadline := time.Now().Add(max)
	var lastErr error
	for {
		dc, err := dtach.Attach(socket)
		if err == nil {
			return dc, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Kill terminates the session (SIGTERM the dtach server group) and removes
// its on-disk files. Equivalent to KillMode(id, false).
func (t *TerminalSessions) Kill(id string) error {
	return t.KillMode(id, false)
}

// KillMode terminates a session. force=false sends SIGTERM, waits up to 5s
// for socket death, then escalates to SIGKILL. force=true sends SIGKILL
// immediately. The terminal record is removed only after death is observed.
// If the process group does not exit, the session is re-registered and an
// error is returned so callers can report failure.
func (t *TerminalSessions) KillMode(id string, force bool) error {
	t.mu.Lock()
	s := t.sessions[id]
	if s != nil {
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	if s == nil {
		return nil
	}

	// In-process test spawner reports the test process PID; never signal it.
	self := os.Getpid()
	if s.PID > 0 && s.PID != self {
		sig := syscall.SIGTERM
		if force {
			sig = syscall.SIGKILL
		}
		_ = syscall.Kill(-s.PID, sig)
		waitDeadline := 5 * time.Second
		if force {
			waitDeadline = 2 * time.Second
		}
		if !t.waitSocketDead(s.Socket, waitDeadline) && !force {
			_ = syscall.Kill(-s.PID, syscall.SIGKILL)
			_ = t.waitSocketDead(s.Socket, 2*time.Second)
		}
	} else {
		// No real process group (or in-process serve): drop the socket so
		// listeners exit, then wait briefly for death.
		if socket, err := unixsocket.Path(s.Socket); err == nil {
			_ = os.Remove(socket)
		}
		_ = t.waitSocketDead(s.Socket, 2*time.Second)
	}

	if t.socketAlive(s.Socket) {
		t.mu.Lock()
		t.sessions[id] = s
		t.mu.Unlock()
		return fmt.Errorf("process did not exit")
	}
	t.removeFiles(id)
	return nil
}

func (t *TerminalSessions) waitSocketDead(path string, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for {
		if !t.socketAlive(path) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Alive reports whether a terminal id currently has a live socket.
func (t *TerminalSessions) Alive(id string) bool {
	t.mu.Lock()
	s := t.sessions[id]
	t.mu.Unlock()
	if s == nil {
		return false
	}
	if t.socketAlive(s.Socket) {
		return true
	}
	// Observed dead — drop stale record.
	t.Forget(id)
	return false
}

// Forget drops a session from memory and disk without signalling it; intended
// for cleanup after the underlying socket is observed dead.
func (t *TerminalSessions) Forget(id string) {
	t.mu.Lock()
	delete(t.sessions, id)
	t.mu.Unlock()
	t.removeFiles(id)
}

func newTerminalID() (string, error) {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return "t" + string(out), nil
}

// spawnSubprocess starts `shelley dtach new` as an out-of-process child so
// it survives shelley restarts (Setsid keeps it detached in its own session).
func (t *TerminalSessions) spawnSubprocess(socket, logFile, cwd, command string, cols, rows uint16, extraEnv []string) (int, error) {
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("terminals: open log: %w", err)
	}
	defer logF.Close()

	args := []string{
		"dtach", "new",
		"-s", socket,
		"-cwd", cwd,
		"-cols", fmt.Sprintf("%d", cols),
		"-rows", fmt.Sprintf("%d", rows),
		"--",
		"bash", "--login", "-c", command,
	}
	cmd := exec.Command(t.exe, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.Stdin = nil
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("terminals: start dtach: %w", err)
	}
	// Waiting for the listener to come up is the caller's job (attachWithRetry).
	// Reap the child in the background so it does not become a zombie when it
	// exits. The PID is captured first so spawning stays non-blocking. If
	// shelley exits, this goroutine dies and the orphaned child reparents to
	// init, which reaps it.
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

// InProcessSpawner runs the dtach server in a goroutine inside the current
// process. Sessions die when this process exits. Intended for tests; blocks
// until the listener is ready.
func InProcessSpawner(socket, logFile, cwd, command string, cols, rows uint16, extraEnv []string) (int, error) {
	ready := make(chan struct{})
	var env []string
	if len(extraEnv) > 0 {
		env = append(os.Environ(), extraEnv...)
	}
	go func() {
		_ = dtach.Serve(dtach.ServerOptions{
			SocketPath: socket,
			Command:    "bash",
			Args:       []string{"--login", "-c", command},
			Dir:        cwd,
			Cols:       cols,
			Rows:       rows,
			Env:        env,
			Ready:      ready,
		})
	}()
	<-ready
	return os.Getpid(), nil
}

// LockAttach returns a function that releases the attach mutex. Callers use
// it to serialize the lookup-or-spawn path.
func (t *TerminalSessions) LockAttach() func() {
	t.attachMu.Lock()
	return t.attachMu.Unlock
}
