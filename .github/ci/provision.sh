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

gitea() { docker exec "$GITEA_CONTAINER" gitea "$@"; }

log() { echo "[provision] $*" >&2; }

# 1. The admin account. `forgejo/gitea admin user create` needs the binary's own
#    working directory, which the image sets up; running as root is fine here
#    because the CLI is invoked through the container's own entrypoint user.
log "creating user $USER_NAME"
gitea admin user create \
  --username "$USER_NAME" \
  --password "$USER_PASS" \
  --email "$USER_MAIL" \
  --admin \
  --must-change-password=false \
  --access-token \
  --token-name "ci-$RANDOM" \
  >/dev/null 2>&1 || log "user already exists (continuing)"

# 2. Clear must-change-password for every account except root. The create above
#    passes --must-change-password=false, but that flag does not clear the bit on
#    an EXISTING row, so a second run of this script on a reused volume would
#    otherwise lock the token out.
gitea admin user must-change-password --all --exclude root --unset >/dev/null 2>&1 || true

# 3. A token with the scopes the E2E actually uses: repository write (push the
#    agent branch), issue write (open/comment/merge the MR), user write (create
#    the scratch repo through /user/repos).
log "minting token"
TOKEN="$(gitea admin user generate-access-token \
  --username "$USER_NAME" \
  --token-name "ci-$(date +%s%N)" \
  --scopes "write:repository,write:issue,write:user,read:user" --raw 2>/dev/null | tr -d '\r' | tail -1)"

if [ -z "${TOKEN}" ]; then
  log "failed to mint a token"
  exit 1
fi

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
