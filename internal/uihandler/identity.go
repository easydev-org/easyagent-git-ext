package uihandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthenticated is returned when the caller presented no usable bearer.
var ErrUnauthenticated = errors.New("a bearer token is required")

// IdentityResolver turns a bearer token into the tenant it belongs to.
//
// WHY ASK THE AGENT: the token is a row in the agent's database, and the mapping token -> tenant
// lives there. An extension cannot verify one on its own — it has no table and no key — so it asks
// the component that issued it. AgentService.GetIdentity already answers exactly this question, so
// no new RPC is needed.
type IdentityResolver struct {
	// AgentURL is the agent's base URL, e.g. http://agent.dev.svc.cluster.local.
	AgentURL string
	Client   *http.Client
}

// Tenant resolves a bearer token to its tenant id.
//
// A failure to REACH the agent is deliberately not "unauthenticated": a network fault must not look
// like an invalid credential, or a brief outage would log everyone out with a misleading message.
func (r *IdentityResolver) Tenant(ctx context.Context, bearer string) (string, error) {
	if strings.TrimSpace(bearer) == "" {
		return "", ErrUnauthenticated
	}
	if r.AgentURL == "" {
		return "", errors.New("identity: no agent URL configured")
	}
	cl := r.Client
	if cl == nil {
		cl = &http.Client{Timeout: 5 * time.Second}
	}

	// Connect with an empty JSON body: GetIdentity takes no arguments.
	url := strings.TrimSuffix(r.AgentURL, "/") + "/agent.v1.AgentService/GetIdentity"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := cl.Do(req)
	if err != nil {
		return "", fmt.Errorf("identity: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrUnauthenticated
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("identity: agent answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Tenant string `json:"tenant"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("identity: malformed response: %w", err)
	}
	if out.Tenant == "" {
		// An ADMIN token has no tenant. This endpoint answers per-tenant data, and there is no
		// meaningful "everything" view for it, so an admin is refused rather than given one.
		return "", ErrUnauthenticated
	}
	return out.Tenant, nil
}
