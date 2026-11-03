package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A stop inside the startup window gives tilt the full grace, so a slow
// but clean shutdown finishes.
func TestUpCancelGivesFullGrace(t *testing.T) {
	old := startupWindow
	startupWindow = 10 * time.Second
	t.Cleanup(func() { startupWindow = old })
	dir := t.TempDir()
	pidFile, cleanFile := filepath.Join(dir, "pid"), filepath.Join(dir, "clean")
	bin := fakeTilt(t, `trap 'sleep 1.5; echo ok > "`+cleanFile+`"; exit 0' TERM
echo $$ > "`+pidFile+`"
while :; do sleep 0.05; done`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if _, err := os.Stat(pidFile); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if _, err := (Tilt{Binary: bin}).Up(ctx, testPort, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Up = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(cleanFile); err != nil {
		t.Fatalf("tilt was killed before its 1.5 s cleanup finished: %v", err)
	}
	if pid := pidFromFile(t, pidFile); !gone(pid) {
		t.Errorf("tilt pid %d survived", pid)
	}
}
