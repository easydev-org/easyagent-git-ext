#!/usr/bin/env bash
# Provision the CI Gitea: one user, one token, one scratch repo.
#
# Prints ONLY the token on stdout (the workflow masks it); everything else goes to
# stderr so the caller can capture the token with a plain command substitution.
#
# WHY the admin CLI rather than the HTTP API:
#   * signup leaves is_active=false, and with no mailer there is no confirmation
#     link — every API token for such a user is then rejected with "user does not
#     exist", which reads like a broken token rather than an inactive account;
#   * signup also sets must_change_password, and with that set EVERY API call is
#     refused with "You must change your password";
#   * the token value is only readable from the API response that creates it,
#     whereas the CLI prints it.
# All three of those cost real debugging time before this script existed.
set -euo pipefail

GITEA_URL="${GITEA_URL:-http://127.0.0.1:3000}"
GITEA_CONTAINER="${GITEA_CONTAINER:-ci-gitea}"
USER_NAME="${USER_NAME:-agentdev}"
USER_PASS="${USER_PASS:-DevPass123!}"
USER_MAIL="${USER_MAIL:-agentdev@example.com}"
REPO="${REPO:-mrflow}"

# Point the CLI at the config the SERVER wrote.
#
# Two things are easy to get wrong here, and both fail in ways that do not name
# the cause:
#   * `docker exec` runs as ROOT while the image runs as uid 1000 (`git`), so the
#     CLI would read a different home/work dir. `-u git` matches the server.
#   * the work dir is a property of the IMAGE VARIANT. This one (gitea/gitea)
#     keeps everything under /data/gitea; /var/lib/gitea belongs to the ROOTLESS
#     variant. Using the wrong one makes `docker exec` itself fail with
#     "chdir to cwd ... no such file or directory" — before gitea even starts —
#     and the CLI's own stderr is empty, so the error looks like a database
#     problem.
GITEA_WORKDIR="${GITEA_WORKDIR:-/data/gitea}"
GITEA_CONFIG="${GITEA_CONFIG:-/data/gitea/conf/app.ini}"
gitea() {
  docker exec -u git -w "$GITEA_WORKDIR" "$GITEA_CONTAINER" \
    /usr/local/bin/gitea --work-path "$GITEA_WORKDIR" --config "$GITEA_CONFIG" "$@"
}

log() { echo "[provision] $*" >&2; }
TMP_ERR="$(mktemp)"
trap 'rm -f "$TMP_ERR" "$CREATE_ERR" "$TOKEN_ERR" 2>/dev/null || true' EXIT

# Prove `docker exec` itself works before blaming the CLI: an exec failure prints
# to the shell's stderr, not the CLI's, so a bare "cannot reach its database" with
# an empty message means exec never ran.
fail_diag() {
  log "diagnostics:"
  log "  container user:  $(docker exec "$GITEA_CONTAINER" id 2>&1 || true)"
  log "  config file:     $(docker exec "$GITEA_CONTAINER" ls -l /data/gitea/conf/app.ini 2>&1 || true)"
  log "  work dir:        $(docker exec "$GITEA_CONTAINER" ls -ld "$GITEA_WORKDIR" 2>&1 || true)"
  log "  binary:          $(docker exec "$GITEA_CONTAINER" ls -l /usr/local/bin/gitea 2>&1 || true)"
  log "  users:           $(docker exec -u git "$GITEA_CONTAINER" id 2>&1 || true)"
}

# 1. The admin account, created through the CLI (see the header for why).
#    First prove the CLI can reach the database at all: if it cannot, every later
#    step fails with an unrelated-looking message.
log "checking the CLI can see the database"
if ! gitea admin user list >/dev/null 2>"$TMP_ERR"; then
  # Capture BOTH streams: a docker-exec failure is reported by docker itself, not
  # by the gitea binary, so redirecting only the command's stderr loses it.
  log "the gitea CLI cannot reach its database: '$(cat "$TMP_ERR")'"
  log "raw attempt: $(docker exec -u git -w "$GITEA_WORKDIR" "$GITEA_CONTAINER" \
      /usr/local/bin/gitea --work-path "$GITEA_WORKDIR" --config "$GITEA_CONFIG" \
      admin user list 2>&1 | head -5)"
  fail_diag
  exit 1
fi

log "creating user $USER_NAME"
CREATE_ERR="$(mktemp)"
gitea admin user create \
  --username "$USER_NAME" \
  --password "$USER_PASS" \
  --email "$USER_MAIL" \
  --admin \
  --must-change-password=false \
  --access-token \
  --token-name "ci-$RANDOM" \
  >/dev/null 2>"$CREATE_ERR" || {
    # "already exists" is expected on a reused volume; anything else is not.
    if grep -qi 'already exists\|user already exists' "$CREATE_ERR"; then
      log "user already exists (continuing)"
    else
      log "create failed: $(cat "$CREATE_ERR")"
    fi
  }
rm -f "$CREATE_ERR"

# 2. Clear must-change-password for every account except root. The create above
#    passes --must-change-password=false, but that flag does not clear the bit on
#    an EXISTING row, so a second run of this script on a reused volume would
#    otherwise lock the token out.
gitea admin user must-change-password --all --exclude root --unset >/dev/null 2>&1 || true

# 3. A token with the scopes the E2E actually uses: repository write (push the
#    agent branch), issue write (open/comment/merge the MR), user write (create
#    the scratch repo through /user/repos).
log "minting token"
# Capture stderr so a CLI rejection is VISIBLE. Silencing it here once cost a CI
# round-trip: the job failed with a bare "exit code 1" and no reason.
TOKEN_ERR="$(mktemp)"
TOKEN="$(gitea admin user generate-access-token \
  --username "$USER_NAME" \
  --token-name "ci-$(date +%s%N)" \
  --scopes "write:repository,write:issue,write:user,read:user" --raw 2>"$TOKEN_ERR" \
  | tr -d '\r' | tail -1)"

if [ -z "${TOKEN}" ]; then
  log "generate-access-token failed: $(cat "$TOKEN_ERR")"
  # Retry without --scopes: older Gitea builds do not accept that flag, and a
  # token with the default (full) scope is fine for CI.
  log "retrying without --scopes"
  TOKEN="$(gitea admin user generate-access-token \
    --username "$USER_NAME" \
    --token-name "ci-$(date +%s%N)" \
    --raw 2>"$TOKEN_ERR" | tr -d '\r' | tail -1)"
  if [ -z "${TOKEN}" ]; then
    log "generate-access-token failed again: $(cat "$TOKEN_ERR")"
    exit 1
  fi
fi
rm -f "$TOKEN_ERR"

# 4. The token must actually work before the test starts; otherwise the E2E fails
#    with a confusing 401 several steps in.
if ! curl -fsS -H "Authorization: token ${TOKEN}" "${GITEA_URL}/api/v1/user" >/dev/null; then
  log "the new token does not authenticate"
  exit 1
fi
log "token authenticates"

# 5. Scratch repository with an initial commit, so the clone has something to
#    clone and the base branch exists for the MR.
if ! curl -fsS -o /dev/null -H "Authorization: token ${TOKEN}" "${GITEA_URL}/api/v1/repos/${USER_NAME}/${REPO}"; then
  log "creating repository ${USER_NAME}/${REPO}"
  curl -fsS -X POST -H "Authorization: token ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"${REPO}\",\"auto_init\":true,\"default_branch\":\"main\",\"private\":false}" \
    "${GITEA_URL}/api/v1/user/repos" >/dev/null
fi

log "ready"
printf '%s' "${TOKEN}"
