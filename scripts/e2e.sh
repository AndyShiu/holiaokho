#!/usr/bin/env bash
# End-to-end test with real clients (mvn, npm, dockerd-in-docker).
# Requires: docker, mvn, npm, go. Uses ports 18081 (http), 15000-15002 (docker), 55432 (postgres).
#   scripts/e2e.sh            run everything
#   KEEP=1 scripts/e2e.sh     leave containers/server running afterwards
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)
H=http://localhost:18081
W=$(mktemp -d)
pass=0; fail=0
ok()   { echo "  ✅ $*"; pass=$((pass+1)); }
bad()  { echo "  ❌ $*"; fail=$((fail+1)); }
check(){ if "$@" >/dev/null 2>&1; then ok "$*"; else bad "$*"; fi; }
BOOT_PW=admin123          # what bootstrap sets
ADMIN_PW='E2e-Runner-2026'  # what we replace it with, as any real operator must
api()  { curl -sf -u "admin:$ADMIN_PW" -H 'Content-Type: application/json' "$@"; }
D()    { docker exec hl-dind docker "$@"; }

echo "== infrastructure"
docker rm -f holiaokho-pg hl-dind >/dev/null 2>&1 || true
docker run -d --name holiaokho-pg -e POSTGRES_USER=holiaokho -e POSTGRES_PASSWORD=holiaokho -e POSTGRES_DB=holiaokho -p 55432:5432 postgres:16-alpine >/dev/null
docker run -d --privileged --name hl-dind -e DOCKER_TLS_CERTDIR= docker:27-dind dockerd --host=unix:///var/run/docker.sock \
  --insecure-registry=host.docker.internal:15000 --insecure-registry=host.docker.internal:15001 --insecure-registry=host.docker.internal:15002 \
  --insecure-registry=host.docker.internal:18081 --registry-mirror=http://host.docker.internal:15000 >/dev/null
for i in $(seq 1 30); do docker exec holiaokho-pg pg_isready -U holiaokho >/dev/null 2>&1 && break; sleep 1; done
rm -rf "$W/blobs"; pkill -f bin/holiaokho 2>/dev/null || true; sleep 0.5
go build -o bin/holiaokho ./cmd/holiaokho && go build -o bin/holiao ./cmd/holiao
HOLIAOKHO_DATABASE_URL="postgres://holiaokho:holiaokho@localhost:55432/holiaokho?sslmode=disable" HOLIAOKHO_LISTEN=":18081" \
  HOLIAOKHO_STORAGE_PATH="$W/blobs" HOLIAOKHO_LOG_LEVEL=info nohup ./bin/holiaokho > "$W/server.log" 2>&1 &
for i in $(seq 1 30); do curl -sf $H/healthz >/dev/null && break; sleep 1; done
check curl -sf $H/readyz

echo "== first login must change the password"
# The bootstrap password came from the environment, so the API refuses
# everything until it is replaced. Prove both halves: locked before, open after.
if curl -sf -u "admin:$BOOT_PW" $H/api/v1/repositories >/dev/null 2>&1; then
  bad "admin should be locked out until the bootstrap password is changed"
else
  ok "admin locked out until the bootstrap password is changed"
fi
if curl -sf -u "admin:$BOOT_PW" -X PUT $H/api/v1/me/password -H 'Content-Type: application/json' \
     -d "{\"current\":\"$BOOT_PW\",\"password\":\"$BOOT_PW\"}" >/dev/null 2>&1; then
  bad "reusing the same password should be rejected"
else
  ok "reusing the same password is rejected"
fi
check curl -sf -u "admin:$BOOT_PW" -X PUT $H/api/v1/me/password -H 'Content-Type: application/json' \
  -d "{\"current\":\"$BOOT_PW\",\"password\":\"$ADMIN_PW\"}"
check api $H/api/v1/repositories

echo "== repositories"
# A fresh install now ships starter repositories (like Nexus); drop them so
# this script owns the whole repository set.
for r in $(api $H/api/v1/repositories | grep -o '"name":"[^"]*"' | cut -d'"' -f4); do
  api -X DELETE "$H/api/v1/repositories/$r" >/dev/null || true
done
check test "$(api $H/api/v1/repositories | grep -o '"name"' | wc -l)" -eq 0
mk() { api -X POST $H/api/v1/repositories -d "$1" >/dev/null; }
mk '{"name":"maven-central","format":"maven","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://repo1.maven.org/maven2/","contentMaxAge":-1},"maven":{"layoutPolicy":"PERMISSIVE","versionPolicy":"RELEASE"}}}'
mk '{"name":"maven-releases","format":"maven","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"},"maven":{"versionPolicy":"RELEASE"}}}'
mk '{"name":"maven-snapshots","format":"maven","type":"hosted","attributes":{"hosted":{"writePolicy":"allow"},"maven":{"versionPolicy":"SNAPSHOT"}}}'
mk '{"name":"maven-public","format":"maven","type":"group","attributes":{"group":{"members":["maven-releases","maven-snapshots","maven-central"]}}}'
mk '{"name":"npm-proxy","format":"npm","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://registry.npmjs.org"}}}'
mk '{"name":"npm-hosted","format":"npm","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
mk '{"name":"npm-group","format":"npm","type":"group","attributes":{"group":{"members":["npm-hosted","npm-proxy"]}}}'
mk '{"name":"docker-hub","format":"docker","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://registry-1.docker.io"},"docker":{"httpPort":15000,"indexType":"HUB"}}}'
mk '{"name":"docker-hosted","format":"docker","type":"hosted","attributes":{"docker":{"httpPort":15001}}}'
mk '{"name":"docker-group","format":"docker","type":"group","attributes":{"group":{"members":["docker-hosted","docker-hub"]},"docker":{"httpPort":15002}}}'
mk '{"name":"pypi-proxy","format":"pypi","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://pypi.org/"}}}'
mk '{"name":"pypi-hosted","format":"pypi","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
mk '{"name":"pypi-group","format":"pypi","type":"group","attributes":{"group":{"members":["pypi-hosted","pypi-proxy"]}}}'
check test "$(api $H/api/v1/repositories | grep -o '"name"' | wc -l)" -eq 13

echo "== maven"
mkdir -p "$W/mvn/src/main/java/demo"; cd "$W/mvn"
cat > settings.xml <<XML
<settings><mirrors><mirror><id>hl</id><url>$H/repository/maven-public/</url><mirrorOf>external:*</mirrorOf></mirror></mirrors>
<servers><server><id>rel</id><username>admin</username><password>E2e-Runner-2026</password></server><server><id>snap</id><username>admin</username><password>E2e-Runner-2026</password></server></servers></settings>
XML
cat > pom.xml <<XML
<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>tw.holiaokho.e2e</groupId><artifactId>lib</artifactId><version>1.0.0</version>
<properties><maven.compiler.source>17</maven.compiler.source><maven.compiler.target>17</maven.compiler.target></properties>
<dependencies><dependency><groupId>org.apache.commons</groupId><artifactId>commons-lang3</artifactId><version>3.17.0</version></dependency></dependencies>
<distributionManagement><repository><id>rel</id><url>$H/repository/maven-releases/</url></repository><snapshotRepository><id>snap</id><url>$H/repository/maven-snapshots/</url></snapshotRepository></distributionManagement></project>
XML
echo 'package demo; public class A { public static String s(){ return org.apache.commons.lang3.StringUtils.capitalize("x"); } }' > src/main/java/demo/A.java
check mvn -q -s settings.xml -Dmaven.repo.local="$W/m2" package
check mvn -q -s settings.xml -Dmaven.repo.local="$W/m2" deploy
if mvn -q -s settings.xml -Dmaven.repo.local="$W/m2" deploy >/dev/null 2>&1; then bad "allow_once must reject redeploy"; else ok "allow_once rejects redeploy"; fi
sed -i.bak 's#<version>1.0.0</version>#<version>1.1.0-SNAPSHOT</version>#' pom.xml
check mvn -q -s settings.xml -Dmaven.repo.local="$W/m2" deploy
check mvn -q -s settings.xml -Dmaven.repo.local="$W/m2" deploy
check curl -sf "$H/repository/maven-public/tw/holiaokho/e2e/lib/maven-metadata.xml" -o "$W/md.xml"
check grep -q "<version>1.1.0-SNAPSHOT</version>" "$W/md.xml"
check grep -q "<release>1.0.0</release>" "$W/md.xml"
cd "$ROOT"

echo "== npm"
mkdir -p "$W/npm/app" "$W/npm/lib"; cd "$W/npm/app"
echo '{"name":"app","version":"1.0.0","private":true,"dependencies":{"lodash":"4.17.21","@babel/core":"7.24.0"}}' > package.json
echo "registry=$H/repository/npm-group/" > .npmrc
check npm install --cache "$W/npmcache" --no-fund --no-audit --loglevel=error
check test -d node_modules/@babel/core
cd "$W/npm/lib"
echo '{"name":"@e2e/lib","version":"1.0.0","main":"index.js"}' > package.json; echo 'module.exports=1' > index.js
printf 'registry=%s/repository/npm-hosted/\n//localhost:18081/repository/npm-hosted/:_auth=%s\n' \
  "$H" "$(printf '%s' "admin:$ADMIN_PW" | base64)" > .npmrc
check npm publish --cache "$W/npmcache" --loglevel=error
cd "$W/npm/app"; check npm install @e2e/lib@1.0.0 --cache "$W/npmcache" --no-fund --no-audit --loglevel=error
check test -f node_modules/@e2e/lib/index.js
cd "$ROOT"

echo "== docker"
for i in $(seq 1 30); do D info >/dev/null 2>&1 && break; sleep 1; done
check D pull alpine:3.20                                    # registry-mirror mode via :15000
check D pull host.docker.internal:15000/busybox:1.36        # port connector, HUB index
check D pull host.docker.internal:18081/docker-hub/alpine:3.19   # path mode
if D login host.docker.internal:15001 -u admin -p wrong >/dev/null 2>&1; then bad "docker login must reject wrong password"; else ok "docker login rejects wrong password"; fi
check D login host.docker.internal:15001 -u admin -p "$ADMIN_PW"
D tag alpine:3.20 host.docker.internal:15001/e2e/alpine:t
check D push host.docker.internal:15001/e2e/alpine:t
D rmi host.docker.internal:15001/e2e/alpine:t >/dev/null
check D pull host.docker.internal:15002/e2e/alpine:t         # via group
check curl -sf http://localhost:15001/v2/e2e/alpine/tags/list

echo "== upstream refusals"
# GHCR refuses a token for a repository that does not exist. That is an answer,
# not an outage: counted as a failure it blocked the whole proxy for 30 seconds,
# so the request right after a typo — for an image that does exist — failed too.
# autoBlock is on because it is what the bug went through; without it this
# test would pass on the old code too.
mk '{"name":"ghcr-e2e","format":"docker","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://ghcr.io","autoBlock":true},"docker":{"indexType":"REGISTRY","pathEnabled":true}}}'
MA='Accept: application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json'
code=$(curl -s -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" -H "$MA" "$H/v2/ghcr-e2e/holiaokho-e2e/does-not-exist/manifests/latest")
if [ "$code" = 404 ]; then ok "a missing image on ghcr is 404"; else bad "a missing image on ghcr is 404 (got $code)"; fi
code=$(curl -s -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" -H "$MA" "$H/v2/ghcr-e2e/getsops/sops/manifests/v3.10.2")
if [ "$code" = 200 ]; then ok "ghcr still answers right after a refusal"; else bad "ghcr still answers right after a refusal (got $code)"; fi

echo "== vulnerability scanning"
# Log4Shell, end to end: the package arrives through the proxy, the scan task
# asks OSV, and the finding is listed with its severity. Needs api.osv.dev,
# like the rest of this script needs the public registries.
check curl -sf -o /dev/null -u "admin:$ADMIN_PW" "$H/repository/maven-central/org/apache/logging/log4j/log4j-core/2.14.1/log4j-core-2.14.1.jar"
check curl -sf -o /dev/null -u "admin:$ADMIN_PW" -X POST "$H/api/v1/tasks/scan-vulnerabilities/run"
found=""
for i in $(seq 1 30); do
  found=$(curl -s -u "admin:$ADMIN_PW" "$H/api/v1/vulnerabilities?q=log4j-core" | python3 -c "import sys,json; print(' '.join(f['id']+':'+f['severity'] for f in json.load(sys.stdin)['items']))" 2>/dev/null || true)
  case "$found" in *GHSA-jfh8-c2jp-5v3q:CRITICAL*) break ;; esac
  sleep 2
done
case "$found" in *GHSA-jfh8-c2jp-5v3q:CRITICAL*) ok "Log4Shell found in log4j-core 2.14.1, rated critical" ;; *) bad "Log4Shell found in log4j-core 2.14.1 (got: $found)" ;; esac
# Aliases are one vulnerability: CVE-2021-44228 must not appear as its own row.
n=$(curl -s -u "admin:$ADMIN_PW" "$H/api/v1/vulnerabilities?q=CVE-2021-44228" | python3 -c "import sys,json; print(json.load(sys.stdin)['total'])")
check test "$n" -eq 1

echo "== oci referrers"
# Signatures, SBOMs and attestations are separate manifests that name the image
# they describe. Exercised with the API directly rather than cosign so the test
# has no network dependency beyond the server itself.
OCI=oci-e2e/img
api -X POST $H/api/v1/repositories -d '{"name":"oci-e2e","format":"docker","type":"hosted","online":true,"attributes":{"hosted":{"writePolicy":"allow"},"docker":{"indexType":"REGISTRY","pathEnabled":true}}}' >/dev/null 2>&1 || true
push_blob() {
  local loc up sep dig
  loc=$(curl -s -u "admin:$ADMIN_PW" -D- -o /dev/null -X POST "$H/v2/$OCI/blobs/uploads/" | awk '/^[Ll]ocation:/{print $2}' | tr -d '\r')
  case "$loc" in http*) up="$loc" ;; *) up="$H$loc" ;; esac
  case "$up" in *\?*) sep='&' ;; *) sep='?' ;; esac
  dig="sha256:$(printf '%s' "$1" | shasum -a 256 | cut -d' ' -f1)"
  curl -s -o /dev/null -u "admin:$ADMIN_PW" -X PUT "${up}${sep}digest=$dig" -H 'Content-Type: application/octet-stream' --data-binary "$1"
  echo "$dig"
}
OCFG='{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}'
OCFG_DIG=$(push_blob "$OCFG")
OIMG=$(printf '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[]}' "$OCFG_DIG" "${#OCFG}")
OIMG_DIG="sha256:$(printf '%s' "$OIMG" | shasum -a 256 | cut -d' ' -f1)"
check curl -sf -u "admin:$ADMIN_PW" -X PUT "$H/v2/$OCI/manifests/v1" -H 'Content-Type: application/vnd.oci.image.manifest.v1+json' --data-binary "$OIMG"

# An unreferenced digest is an empty index with 200 — a 404 would tell clients
# the endpoint is unsupported.
if [ "$(curl -s -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" "$H/v2/$OCI/referrers/$OIMG_DIG")" = "200" ]; then
  ok "referrers of an unreferenced digest is 200"
else bad "referrers of an unreferenced digest is 200"; fi

OE='{}'
OE_DIG=$(push_blob "$OE")
OSIG=$(printf '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","artifactType":"application/vnd.dev.cosign.simplesigning.v1+json","config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"%s","size":%d},"layers":[],"subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"%s","size":%d}}' "$OE_DIG" "${#OE}" "$OIMG_DIG" "${#OIMG}")
OSIG_DIG="sha256:$(printf '%s' "$OSIG" | shasum -a 256 | cut -d' ' -f1)"
if curl -s -D "$W/ocihdr.txt" -o /dev/null -u "admin:$ADMIN_PW" -X PUT "$H/v2/$OCI/manifests/$OSIG_DIG" -H 'Content-Type: application/vnd.oci.image.manifest.v1+json' --data-binary "$OSIG" && grep -qi "^oci-subject:" "$W/ocihdr.txt"; then
  ok "pushing a manifest with a subject returns OCI-Subject"
else bad "pushing a manifest with a subject returns OCI-Subject"; fi

n=$(curl -s -u "admin:$ADMIN_PW" "$H/v2/$OCI/referrers/$OIMG_DIG" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['manifests']))")
check test "$n" -eq 1
n=$(curl -s -u "admin:$ADMIN_PW" "$H/v2/$OCI/referrers/$OIMG_DIG?artifactType=application/spdx%2Bjson" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['manifests']))")
check test "$n" -eq 0

# The same relationship, through the endpoint the web UI asks. It differs from
# the registry API in one way that matters: it names the kind of each artifact,
# so the drawer can say "Signature" instead of showing a media type.
PKG_ID=$(curl -s -u "admin:$ADMIN_PW" "$H/api/v1/search?q=img" | python3 -c "import sys,json; d=json.load(sys.stdin); print((d['items'] if isinstance(d, dict) else d)[0]['id'])" 2>/dev/null)
kind=$(curl -s -u "admin:$ADMIN_PW" "$H/api/v1/packages/$PKG_ID/referrers" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d[0]['kind'] if d else 'none')" 2>/dev/null)
check test "$kind" = signature

# Deleting the referrer has to clear the index, or it keeps being advertised.
curl -s -o /dev/null -u "admin:$ADMIN_PW" -X DELETE "$H/v2/$OCI/manifests/$OSIG_DIG"
n=$(curl -s -u "admin:$ADMIN_PW" "$H/v2/$OCI/referrers/$OIMG_DIG" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['manifests']))")
check test "$n" -eq 0

echo "== pypi"
if command -v python3 >/dev/null; then
  python3 -m venv "$W/venv" && check "$W/venv/bin/pip" install -q --index-url $H/repository/pypi-group/simple/ --no-cache-dir six
  mkdir -p "$W/py" && cd "$W/py" && python3 - <<PY
import zipfile
with zipfile.ZipFile('e2epkg-0.1.0-py3-none-any.whl','w') as z:
    z.writestr('e2epkg.py','X=1\n'); z.writestr('e2epkg-0.1.0.dist-info/METADATA','Metadata-Version: 2.1\nName: e2epkg\nVersion: 0.1.0\n')
    z.writestr('e2epkg-0.1.0.dist-info/WHEEL','Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\n'); z.writestr('e2epkg-0.1.0.dist-info/RECORD','')
PY
  check curl -sf -u "admin:$ADMIN_PW" -F ":action=file_upload" -F "name=e2epkg" -F "version=0.1.0" -F "content=@e2epkg-0.1.0-py3-none-any.whl" $H/repository/pypi-hosted/
  check "$W/venv/bin/pip" install -q --index-url $H/repository/pypi-group/simple/ --no-cache-dir e2epkg
  cd "$ROOT"
fi

echo "== cli"
export HOLIAO_CONFIG="$W/holiao.yaml"
check "$ROOT/bin/holiao" login $H -u admin -p "$ADMIN_PW"
check "$ROOT/bin/holiao" repo ls
check "$ROOT/bin/holiao" search lib
check "$ROOT/bin/holiao" task run blob-gc
storage_test() { api -X POST $H/api/v1/storages/test -d "$1" | grep -q "\"ok\":$2"; }
check storage_test "{\"type\":\"fs\",\"config\":{\"path\":\"$W/probe\"}}" true
check storage_test '{"type":"s3","config":{"endpoint":"http://127.0.0.1:1","bucket":"b"}}' false

echo
echo "passed: $pass  failed: $fail  (server log: $W/server.log)"
if [ -z "${KEEP:-}" ]; then pkill -f bin/holiaokho || true; docker rm -f holiaokho-pg hl-dind >/dev/null; fi
[ "$fail" -eq 0 ]
