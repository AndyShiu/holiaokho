# source this: dev helpers for hitting the local server
#
# Override the password if you changed it:  HL_PW=... source scripts/dev-env.sh
# A freshly bootstrapped server refuses everything until the admin password is
# replaced, so `first_login` does that once and the rest then works.
export H=http://localhost:18081
export HL_PW="${HL_PW:-admin123}"
api() { curl -s -u "admin:$HL_PW" -H 'Content-Type: application/json' "$@"; }
first_login() {
  local new="${1:?usage: first_login <new-password>}"
  curl -sf -u "admin:$HL_PW" -X PUT "$H/api/v1/me/password" \
    -H 'Content-Type: application/json' \
    -d "{\"current\":\"$HL_PW\",\"password\":\"$new\"}" && export HL_PW="$new" && echo "ok"
}
post_repo() { api -X POST "$H/api/v1/repositories" -d "$1"; echo; }
