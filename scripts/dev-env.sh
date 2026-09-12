# source this: dev helpers for hitting the local server
export H=http://localhost:18081
api() { curl -s -u admin:admin123 -H 'Content-Type: application/json' "$@"; }
post_repo() { api -X POST "$H/api/v1/repositories" -d "$1"; echo; }
