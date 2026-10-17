// A tiny driver: connects an easybus Agent to a running git-ext, calls the
// git-* tools against a local bare repo, and prints the outcomes.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	easybus "github.com/easydev-org/easybus-sdk/agent"
	"github.com/easydev-org/easybus-sdk/transport/nats"
)

func main() {
	url := envOr("NATS_URL", "nats://127.0.0.1:4222")
	remote := os.Args[1]
	bus, err := nats.Connect(url)
	if err != nil {
		panic(err)
	}
	defer bus.Close()
	a := easybus.New(bus)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	call := func(tool string, args map[string]any) {
		tr, err := a.CallTool(ctx, "t1", "sess-1", "git", tool, fmt.Sprintf("c-%d", time.Now().UnixNano()), args)
		if err != nil {
			fmt.Printf("%s: ERR %v\n", tool, err)
			return
		}
		if tr.Error != nil {
			fmt.Printf("%s: toolerr %v\n", tool, tr.Error)
			return
		}
		fmt.Printf("%s: %s\n", tool, tr.Content)
	}

	call("git-clone", map[string]any{"remote": remote, "depth": float64(0)})
	call("git-status", map[string]any{"remote": remote})
	call("git-log", map[string]any{"remote": remote, "limit": float64(3)})
	call("git-branch", map[string]any{"remote": remote})
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
