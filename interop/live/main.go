// live e2e driver for gitea-ext: connects an easybus agent to a running gitea-ext,
// points it at a sandbox worker (WORKER_TOKEN, already claimed) + a real remote,
// and drives git-clone/status/log/branch. git runs IN the sandbox.
//
//	usage: live <nats-url> <worker-url> <worker-token> [remote]
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	easybusagent "github.com/easydev-org/easybus-sdk/agent"
	"github.com/easydev-org/easybus-sdk/transport/nats"
)

func main() {
	natsURL, workerURL, workerTok := os.Args[1], os.Args[2], os.Args[3]
	remote := "https://github.com/octocat/Hello-World.git"
	if len(os.Args) > 4 {
		remote = os.Args[4]
	}

	bus, err := nats.Connect(natsURL)
	if err != nil {
		panic(err)
	}
	defer bus.Close()
	a := easybusagent.New(bus)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, _ = a.Discover(ctx, 1500)
	a.ServeConfig(true)
	must(a.SetConfig(ctx, "t1", "git", "sandboxes",
		fmt.Sprintf(`[{"name":"pod-a","url":%q,"token":%q}]`, workerURL, workerTok), "", nil, nil))
	must(a.SetConfig(ctx, "t1", "git", "servers", "[]", "", nil, nil))

	call := func(tool string, args map[string]any) string {
		tr, err := a.CallTool(ctx, "t1", "s", "git", tool, fmt.Sprintf("c-%d", time.Now().UnixNano()), args)
		if err != nil {
			fmt.Printf("%s: ERR %v\n", tool, err)
			return ""
		}
		if tr.Error != nil {
			fmt.Printf("%s: toolerr %v\n", tool, tr.Error)
			return ""
		}
		fmt.Printf("%s: %s\n", tool, strings.TrimSpace(tr.Content))
		return tr.Content
	}
	base := map[string]any{"sandbox": "pod-a", "path": "repo"}
	call("git-clone", map[string]any{"sandbox": "pod-a", "path": "repo", "remote": remote, "depth": float64(1)})
	call("git-status", base)
	call("git-log", base)
	call("git-branch", base)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
