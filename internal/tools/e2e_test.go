package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	workerv1 "github.com/easydev-org/easyagent-gitea-ext/gen/worker/v1"
	"github.com/easydev-org/easyagent-gitea-ext/gen/worker/v1/workerv1connect"
	"github.com/easydev-org/easyagent-gitea-ext/internal/sandbox"
)

// fakeWorker records commands and answers as if git ran in a real sandbox.
type fakeWorker struct {
	workerv1connect.UnimplementedWorkerServiceHandler
	commands []string
	stdin    map[string]string
	failRepo bool
}

func (f *fakeWorker) Execute(_ context.Context, req *workerv1.ExecuteRequest) (*workerv1.ExecuteResponse, error) {
	f.commands = append(f.commands, req.GetCommand())
	return &workerv1.ExecuteResponse{JobId: fmt.Sprintf("j%d", len(f.commands))}, nil
}
func (f *fakeWorker) JobStdin(_ context.Context, req *workerv1.JobStdinRequest) (*workerv1.JobStdinResponse, error) {
	if f.stdin == nil {
		f.stdin = map[string]string{}
	}
	f.stdin[req.GetJobId()] = string(req.GetData())
	return &workerv1.JobStdinResponse{Ok: true}, nil
}
func (f *fakeWorker) JobWait(context.Context, *workerv1.JobWaitRequest) (*workerv1.JobWaitResponse, error) {
	if f.failRepo {
		return &workerv1.JobWaitResponse{State: "failed", ExitCode: 1}, nil
	}
	return &workerv1.JobWaitResponse{State: "done", ExitCode: 0}, nil
}
func (f *fakeWorker) JobOutput(_ context.Context, req *workerv1.JobOutputRequest) (*workerv1.JobOutputResponse, error) {
	// Answer rev-parse checks so EnsureRepo passes.
	return &workerv1.JobOutputResponse{Lines: []string{".git"}, TotalLines: 1, Done: true}, nil
}

func newServer(t *testing.T) (*fakeWorker, string) {
	t.Helper()
	f := &fakeWorker{}
	srv := connect.NewServer()
	workerv1connect.RegisterWorkerServiceHandler(srv, f)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv, connecthttp.WithReadMaxBytes(0))
	ts := httptest.NewUnstartedServer(mux)
	p := new(http.Protocols)
	p.SetHTTP1(true)
	ts.Config.Protocols = p
	ts.Start()
	t.Cleanup(ts.Close)
	return f, ts.URL
}

func reg(t *testing.T, url string) map[string]Handler {
	t.Helper()
	return Registry(Deps{
		Cache:  sandbox.NewClientCache(4),
		Locale: func(string, string) string { return "en" },
		GetConfig: func(name, _, _ string) any {
			switch name {
			case "sandboxes":
				return `[{"name":"pod-a","url":"` + url + `","token":"t"}]`
			case "servers":
				return `[{"host":"github.com","kind":"github","token":"ghp_x"}]`
			}
			return ""
		},
	})
}

func TestStatusRunsGitInSandbox(t *testing.T) {
	f, url := newServer(t)
	r := reg(t, url)
	if _, err := r["git-status"](context.Background(), map[string]any{"sandbox": "pod-a", "path": "/work/repo"}, "s", "ten"); err != nil {
		t.Fatal(err)
	}
	// First command is the repo check, second is the status.
	joined := strings.Join(f.commands, " | ")
	if !strings.Contains(joined, "rev-parse --git-dir") {
		t.Errorf("no repo check: %v", f.commands)
	}
	if !strings.Contains(joined, "status") {
		t.Errorf("no status: %v", f.commands)
	}
}

func TestNonRepoPathRejected(t *testing.T) {
	// Make the repo check fail (exit 1).
	f2 := &fakeWorker{failRepo: true}
	srv := connect.NewServer()
	workerv1connect.RegisterWorkerServiceHandler(srv, f2)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv, connecthttp.WithReadMaxBytes(0))
	ts := httptest.NewUnstartedServer(mux)
	p := new(http.Protocols)
	p.SetHTTP1(true)
	ts.Config.Protocols = p
	ts.Start()
	t.Cleanup(ts.Close)
	r := reg(t, ts.URL)
	if _, err := r["git-status"](context.Background(), map[string]any{"sandbox": "pod-a", "path": "/nope"}, "s", "ten"); err == nil {
		t.Error("non-repo path should be rejected")
	}
}

func TestMissingPathRejected(t *testing.T) {
	_, url := newServer(t)
	r := reg(t, url)
	if _, err := r["git-status"](context.Background(), map[string]any{"sandbox": "pod-a"}, "s", "ten"); err == nil {
		t.Error("missing path should be rejected")
	}
}

func TestUnknownSandboxRejected(t *testing.T) {
	_, url := newServer(t)
	r := reg(t, url)
	if _, err := r["git-status"](context.Background(), map[string]any{"sandbox": "nope", "path": "/w/r"}, "s", "ten"); err == nil {
		t.Error("unknown sandbox should be rejected")
	}
}

func TestClonePassesRemoteAndPath(t *testing.T) {
	f, url := newServer(t)
	r := reg(t, url)
	if _, err := r["git-clone"](context.Background(), map[string]any{"sandbox": "pod-a", "path": "/w/repo", "remote": "https://github.com/x/y.git", "depth": float64(1)}, "s", "ten"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.commands, " | ")
	if !strings.Contains(joined, "clone") || !strings.Contains(joined, "github.com/x/y.git") {
		t.Errorf("clone cmd = %v", f.commands)
	}
}

// TestTokenNeverInCommand proves the SECRET contract: when a host is configured
// with a token, the token is NOT in the Execute command (which the worker
// persists + ListJobs exposes) and NOT in any env — it rides stdin only.
func TestTokenNeverInCommand(t *testing.T) {
	f, url := newServer(t)
	r := reg(t, url)
	if _, err := r["git-clone"](context.Background(), map[string]any{
		"sandbox": "pod-a", "path": "/w/repo", "remote": "https://github.com/x/y.git", "depth": float64(1),
	}, "s", "ten"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.commands {
		if strings.Contains(c, "ghp_x") {
			t.Fatalf("token LEAKED into command: %q", c)
		}
	}
	// The token must have arrived via stdin.
	found := false
	for _, v := range f.stdin {
		if strings.Contains(v, "ghp_x") {
			found = true
		}
	}
	if !found {
		t.Error("token did not arrive via stdin")
	}
	// The command still reads GIT_TOKEN (the stdin-fed variable).
	joined := strings.Join(f.commands, " | ")
	if !strings.Contains(joined, "read -r GIT_TOKEN") {
		t.Errorf("no stdin read in command: %v", f.commands)
	}
}
