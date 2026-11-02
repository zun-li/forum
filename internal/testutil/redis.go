package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Start an isolated Redis listening only on a private Unix socket. No data
// or config from a running development instance is used.
func Redis(t *testing.T) *redis.Client {
	t.Helper()
	server, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("install redis-server to run Redis integration tests")
	}
	// macOS has a 104-byte socket path limit; t.TempDir can exceed it.
	dir, err := os.MkdirTemp("/tmp", "forum-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(server, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no", "--dir", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	t.Cleanup(func() { client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return client
		}
		if ctx.Err() != nil {
			t.Fatal("private redis-server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
