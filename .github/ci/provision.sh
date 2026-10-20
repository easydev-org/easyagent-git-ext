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
trap 'rm -f "$TMP_ERR" "$CREATE_OUT" "$TOKEN_OUT" 2>/dev/null || true' EXIT

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
# Capture BOTH streams into one file: the CLI reports success on stdout and some
# failures there too, so watching only stderr showed an empty message.
CREATE_OUT="$(mktemp)"
gitea admin user create \
  --username "$USER_NAME" \
  --password "$USER_PASS" \
  --email "$USER_MAIL" \
  --admin \
  --must-change-password=false \
  >"$CREATE_OUT" 2>&1 || true
log "create said: $(tr '\n' ' ' <"$CREATE_OUT")"
if ! grep -qi 'already exists\|successfully created' "$CREATE_OUT"; then
  log "create did not report success"
  fail_diag
  exit 1
fi
rm -f "$CREATE_OUT"

# 2. Clear must-change-password for every account except root. The create above
#    passes --must-change-password=false, but that flag does not clear the bit on
#    an EXISTING row, so a second run of this script on a reused volume would
#    otherwise lock the token out.
gitea admin user must-change-password --all --exclude root --unset >/dev/null 2>&1 || true

# 3. A token with the scopes the E2E actually uses: repository write (push the
#    agent branch), issue write (open/comment/merge the MR), user write (create
#    the scratch repo through /user/repos).
log "minting token"
# Capture BOTH streams. Silencing stderr once cost a CI round-trip that ended in a
# bare "exit code 1" with no reason at all.
TOKEN_OUT="$(mktemp)"
try_token() {
  : >"$TOKEN_OUT"
  gitea admin user generate-access-token \
    --username "$USER_NAME" \
    --token-name "ci-$(date +%s%N)" \
    "$@" --raw >"$TOKEN_OUT" 2>&1 || true
  # The token is the last line that looks like one; everything else is chatter.
  grep -oE '^[A-Za-z0-9]{20,}$' "$TOKEN_OUT" | tail -1
}

TOKEN="$(try_token --scopes 'write:repository,write:issue,write:user,read:user')"
if [ -z "${TOKEN}" ]; then
  log "generate-access-token (with --scopes) said: $(tr '\n' ' ' <"$TOKEN_OUT")"
  # Retry without --scopes: not every Gitea build accepts that flag, and a token
  # with the default (full) scope is fine for CI.
  log "retrying without --scopes"
  TOKEN="$(try_token)"
  if [ -z "${TOKEN}" ]; then
    log "generate-access-token (no --scopes) said: $(tr '\n' ' ' <"$TOKEN_OUT")"
    fail_diag
    exit 1
  fi
fi
rm -f "$TOKEN_OUT"

# 4. The token must actually work before the test starts; otherwise the E2E fails
#    with a confusing 401 several steps in.
if ! curl -fsS -H "Authorization: token ${TOKEN}" "${GITEA_URL}/api/v1/user" >/dev/null; then
  log "the new token does not authenticate"
  exit 1
fi
log "token authenticates"

# 4b. Prove the account can actually WRITE, not just authenticate.
#
# `remote: Verify` is what Gitea answers to a push when the account or its email
# is not in the state a write requires — and a token that authenticates tells you
# nothing about that. Testing the write itself is the only check that catches it
# here rather than two steps later inside the E2E.
log "user state: $(curl -s -H "Authorization: token ${TOKEN}" "${GITEA_URL}/api/v1/user" \
  | tr ',' '\n' | grep -E '"restricted"|"active"|"prohibit_login"|"login"' | tr '\n' ' ')"

# The admin CLI is the only lever that can flip the email state without a mailer.
# Run it unconditionally: it is a no-op when the account is already fine, and it
# is the difference between "authenticates" and "can write".
gitea admin user must-change-password --all --exclude root --unset >/dev/null 2>&1 || true
case "${VERIFY_EMAIL_VIA_SQL:-}" in
  sqlite)
    # Last resort: the ORM column Gitea consults before allowing a write. Only
    # used when the CLI cannot express it.
    log "marking email activated via sqlite"
    docker exec "$GITEA_CONTAINER" \
      sqlite3 /data/gitea/gitea.db \
      "update email_address set is_activated=1, valid=true where lower_email like '%${USER_MAIL}%';" \
      2>&1 | tail -2 || true
    ;;
esac

# 5. Scratch repository with an initial commit, so the clone has something to
#    clone and the base branch exists for the MR.
# `curl -f` returns non-zero on 404, which is exactly the "does it exist" answer
# we want — but under `set -e` a non-zero anywhere in a compound command can end
# the script. Capture the status explicitly so the branch is decided by us.
repo_status="$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: token ${TOKEN}" \
  "${GITEA_URL}/api/v1/repos/${USER_NAME}/${REPO}" || echo 000)"

if [ "${repo_status}" != "200" ]; then
  log "creating repository ${USER_NAME}/${REPO} (current status ${repo_status})"
  create_out="$(curl -s -w '\n%{http_code}' -X POST -H "Authorization: token ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"${REPO}\",\"auto_init\":true,\"default_branch\":\"main\",\"private\":false}" \
    "${GITEA_URL}/api/v1/user/repos" || true)"
  create_code="$(printf '%s' "${create_out}" | tail -1)"
  log "create returned ${create_code}: $(printf '%s' "${create_out}" | head -1 | cut -c1-200)"
  if [ "${create_code}" != "201" ]; then
    log "could not create the scratch repository"
    exit 1
  fi
fi

# The repository must be readable BEFORE the E2E clones it, or the test fails on a
# clone timeout instead of on this step.
if [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: token ${TOKEN}" \
      "${GITEA_URL}/api/v1/repos/${USER_NAME}/${REPO}" || echo 000)" != "200" ]; then
  log "the scratch repository is not readable"
  exit 1
fi
log "repository ready"

# Hand the token back through a FILE, not stdout.
#
# Printing it on stdout works under `t=$(script)` but is fragile: any line that
# reaches the runner's stdout is parsed for workflow commands, and a bare token
# is rejected with "Invalid format '<token>'" — which looks like a leak and stops
# the job. A file cannot be mistaken for log output.
#   $1 = optional path to write the token to (default: a temp file we report).
if [ -n "${1:-}" ]; then
  printf '%s' "${TOKEN}" >"$1"
  chmod 600 "$1"
  log "token written to $1"
else
  log "TOKEN_PATH is not set; pass a path as the first argument"
  exit 1
fi
