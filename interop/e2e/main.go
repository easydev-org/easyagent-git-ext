// e2e: a stub easyworker (real WorkerService over Connect) that actually runs
// git in a temp dir, plus a driver that calls gitea-ext's tools through an
// easybus agent. Proves the whole path: agent -> gitea-ext -> sandbox -> git.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	easybusagent "github.com/easydev-org/easybus-sdk/agent"
	"github.com/easydev-org/easybus-sdk/transport/nats"
	workerv1 "github.com/easydev-org/easyagent-gitea-ext/gen/worker/v1"
	"github.com/easydev-org/easyagent-gitea-ext/gen/worker/v1/workerv1connect"
)

type stub struct {
	workerv1connect.UnimplementedWorkerServiceHandler
	root string
	out  map[string][]string
}

func (s *stub) Execute(_ context.Context, req *workerv1.ExecuteRequest) (*workerv1.ExecuteResponse, error) {
	cmd := exec.Command("sh", "-c", req.GetCommand())
	cmd.Dir = s.root
	b, _ := cmd.CombinedOutput()
	id := fmt.Sprintf("j%d", time.Now().UnixNano())
	s.out[id] = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(s.out[id]) == 0 {
		s.out[id] = []string{""}
	}
	return &workerv1.ExecuteResponse{JobId: id}, nil
}
func (s *stub) JobWait(_ context.Context, req *workerv1.JobWaitRequest) (*workerv1.JobWaitResponse, error) {
	return &workerv1.JobWaitResponse{State: "done", ExitCode: 0}, nil
}
func (s *stub) JobOutput(_ context.Context, req *workerv1.JobOutputRequest) (*workerv1.JobOutputResponse, error) {
	lines := s.out[req.GetJobId()]
	return &workerv1.JobOutputResponse{Lines: lines, TotalLines: int32(len(lines)), Done: true}, nil
}

func main() {
	root := os.Args[1]        // sandbox workspace root
	natsURL := os.Args[2]     // nats url
	remote := os.Args[3]      // git remote (local bare repo)

	st := &stub{root: root, out: map[string][]string{}}
	srv := connect.NewServer()
	workerv1connect.RegisterWorkerServiceHandler(srv, st)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv, connecthttp.WithReadMaxBytes(0))
	go func() { _ = http.ListenAndServe("127.0.0.1:18999", mux) }()
	time.Sleep(300 * time.Millisecond)

	bus, err := nats.Connect(natsURL)
	if err != nil { panic(err) }
	defer bus.Close()
	a := easybusagent.New(bus)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Configure the extension: point at the stub worker + a local git server.
	_, _ = a.Discover(ctx, 1500)
	a.ServeConfig(true)
	sandboxes := `[{"name":"pod-a","url":"http://127.0.0.1:18999","token":""}]`
	if err := a.SetConfig(ctx, "t1", "git", "sandboxes", sandboxes, "", nil, nil); err != nil {
		fmt.Println("setConfig sandboxes:", err)
	}
	if err := a.SetConfig(ctx, "t1", "git", "servers", "[]", "", nil, nil); err != nil {
		fmt.Println("setConfig servers:", err)
	}

	call := func(tool string, args map[string]any) {
		tr, err := a.CallTool(ctx, "t1", "s", "git", tool, fmt.Sprintf("c-%d", time.Now().UnixNano()), args)
		if err != nil { fmt.Printf("%s: ERR %v\n", tool, err); return }
		if tr.Error != nil { fmt.Printf("%s: toolerr %v\n", tool, tr.Error); return }
		fmt.Printf("%s: %s\n", tool, strings.TrimSpace(tr.Content))
	}

	// clone into the sandbox at path "repo"
	call("git-clone", map[string]any{"sandbox": "pod-a", "path": "repo", "remote": remote, "depth": float64(0)})
	call("git-status", map[string]any{"sandbox": "pod-a", "path": "repo"})
	call("git-log", map[string]any{"sandbox": "pod-a", "path": "repo"})
	// non-repo path must be rejected
	call("git-status", map[string]any{"sandbox": "pod-a", "path": "notarepo"})
}
