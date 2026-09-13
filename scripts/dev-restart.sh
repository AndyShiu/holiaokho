#!/bin/sh
# Rebuild and restart the local dev server (Postgres from `docker run`, see docs).
# Also serves HTTPS on :18443 when scratchpad TLS certs exist (needed by Terraform tests).
cd "$(dirname "$0")/.."
pkill -f bin/holiaokho 2>/dev/null; sleep 0.5
go build -o bin/holiaokho ./cmd/holiaokho || exit 1
T=/private/tmp/claude-501/-Users-andyshiu-work-05-private-holiao/56d79738-2b23-42e7-afa5-8b3d145456ed/scratchpad/tls
if [ -f "$T/server.crt" ]; then export HOLIAOKHO_TLS_LISTEN=":18443" HOLIAOKHO_TLS_CERT="$T/server.crt" HOLIAOKHO_TLS_KEY="$T/server.key"; fi
HOLIAOKHO_DATABASE_URL="${HOLIAOKHO_DATABASE_URL:-postgres://holiaokho:holiaokho@localhost:55432/holiaokho?sslmode=disable}" \
HOLIAOKHO_LISTEN="${HOLIAOKHO_LISTEN:-:18081}" HOLIAOKHO_STORAGE_PATH="./data/blobs" HOLIAOKHO_LOG_LEVEL="${HOLIAOKHO_LOG_LEVEL:-debug}" \
nohup ./bin/holiaokho > data/server.log 2>&1 &
sleep 1.5
curl -sf localhost:18081/healthz >/dev/null && echo "server restarted" || { echo "server failed"; tail -20 data/server.log; exit 1; }
