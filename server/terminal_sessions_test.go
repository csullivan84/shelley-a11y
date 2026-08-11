package server

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// processExists uses the portable Unix signal 0 probe. It remains true for a
// zombie until Wait reaps it, then becomes false on both macOS and Linux.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// TestSpawnSubprocessReapsChild verifies that a spawned dtach child that exits
// is reaped rather than left as a zombie. Regression test for the
// Release()-without-Wait() bug that produced "[shelley] <defunct>" processes.
//
// The child here is the true executable from PATH, which exits immediately
// (ignoring the dtach args). With the bug, the child becomes a zombie and stays
// that way for the lifetime of the test process. With the fix, the background
// Wait reaps it and the signal 0 probe reports that it no longer exists.
func TestSpawnSubprocessReapsChild(t *testing.T) {
	dir := t.TempDir()
	ts, err := NewTerminalSessions(dir, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("NewTerminalSessions: %v", err)
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("find true executable: %v", err)
	}
	ts.exe = truePath

	socket := filepath.Join(dir, "sock")
	logFile := filepath.Join(dir, "log")
	pid, err := ts.spawnSubprocess(socket, logFile, dir, "echo hi", 80, 24, nil)
	if err != nil {
		t.Fatalf("spawnSubprocess: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive pid, got %d", pid)
	}

	// Poll until the child is fully reaped. A child stuck in the zombie state
	// remains visible to signal 0, so it fails the test.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return // reaped — success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("child pid %d was not reaped", pid)
}

func TestTerminalListPrunesDeadSessions(t *testing.T) {
	dir := t.TempDir()
	ts, err := NewTerminalSessions(dir, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	session := &TerminalSession{
		ID: "tdead", Socket: filepath.Join(dir, "missing.sock"), LogFile: filepath.Join(dir, "tdead.log"),
	}
	ts.sessions[session.ID] = session
	if got := ts.List(); len(got) != 0 {
		t.Fatalf("dead terminal remained in list: %+v", got)
	}
}
