package forge

import (
	"context"
	"os"
	"testing"
	"time"
)

/**
 * SyncUpstream against a REAL forge and a REAL upstream.
 *
 * Skipped unless both are configured: the interesting behaviour — that a moved upstream is followed
 * by a fast-forward and an unmoved one is left alone — cannot be observed without two repositories.
 *
 *	FORGE_LIVE_URL=http://forgejo.dev.svc.cluster.local FORGE_LIVE_TOKEN=... \
 *	FORGE_LIVE_REPO=agentdev/mirror-probe \
 *	UPSTREAM_LIVE_URL=https://github.com/easydev-org/test.git UPSTREAM_LIVE_TOKEN=... \
 *	go test ./internal/forge -run Live
 */
func TestSyncUpstreamLive(t *testing.T) {
	base := os.Getenv("FORGE_LIVE_URL")
	tok := os.Getenv("FORGE_LIVE_TOKEN")
	repo := os.Getenv("FORGE_LIVE_REPO")
	up := os.Getenv("UPSTREAM_LIVE_URL")
	upTok := os.Getenv("UPSTREAM_LIVE_TOKEN")
	if base == "" || tok == "" || repo == "" || up == "" {
		t.Skip("set FORGE_LIVE_* and UPSTREAM_LIVE_URL to run")
	}
	c, err := New(base, tok, base+"/"+repo+".git")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// First call: syncing a branch that is already level must report no movement.
	moved, local, upstream, err := c.SyncUpstream(ctx, "main", up, upTok)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	t.Logf("moved=%v local=%s upstream=%s", moved, shortSHA(local), shortSHA(upstream))

	// Second call must be a no-op: whatever the first did, the two are level now, so nothing moves.
	// This is the behaviour a reader depends on — opening a protected branch repeatedly must not
	// rewrite it every time.
	moved2, local2, upstream2, err := c.SyncUpstream(ctx, "main", up, upTok)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if moved2 {
		t.Errorf("nothing should have moved: local=%s upstream=%s", shortSHA(local2), shortSHA(upstream2))
	}
	if local2 != upstream2 {
		t.Errorf("after syncing, local (%s) should equal upstream (%s)", shortSHA(local2), shortSHA(upstream2))
	}
}
