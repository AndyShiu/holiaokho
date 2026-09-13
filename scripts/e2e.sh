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
api()  { curl -sf -u admin:admin123 -H 'Content-Type: application/json' "$@"; }
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

echo "== repositories"
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
<servers><server><id>rel</id><username>admin</username><password>admin123</password></server><server><id>snap</id><username>admin</username><password>admin123</password></server></servers></settings>
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
printf 'registry=%s/repository/npm-hosted/\n//localhost:18081/repository/npm-hosted/:_auth=YWRtaW46YWRtaW4xMjM=\n' "$H" > .npmrc
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
check D login host.docker.internal:15001 -u admin -p admin123
D tag alpine:3.20 host.docker.internal:15001/e2e/alpine:t
check D push host.docker.internal:15001/e2e/alpine:t
D rmi host.docker.internal:15001/e2e/alpine:t >/dev/null
check D pull host.docker.internal:15002/e2e/alpine:t         # via group
check curl -sf http://localhost:15001/v2/e2e/alpine/tags/list

echo "== pypi"
if command -v python3 >/dev/null; then
  python3 -m venv "$W/venv" && check "$W/venv/bin/pip" install -q --index-url $H/repository/pypi-group/simple/ --no-cache-dir six
  mkdir -p "$W/py" && cd "$W/py" && python3 - <<PY
import zipfile
with zipfile.ZipFile('e2epkg-0.1.0-py3-none-any.whl','w') as z:
    z.writestr('e2epkg.py','X=1\n'); z.writestr('e2epkg-0.1.0.dist-info/METADATA','Metadata-Version: 2.1\nName: e2epkg\nVersion: 0.1.0\n')
    z.writestr('e2epkg-0.1.0.dist-info/WHEEL','Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\n'); z.writestr('e2epkg-0.1.0.dist-info/RECORD','')
PY
  check curl -sf -u admin:admin123 -F ":action=file_upload" -F "name=e2epkg" -F "version=0.1.0" -F "content=@e2epkg-0.1.0-py3-none-any.whl" $H/repository/pypi-hosted/
  check "$W/venv/bin/pip" install -q --index-url $H/repository/pypi-group/simple/ --no-cache-dir e2epkg
  cd "$ROOT"
fi

echo "== cli"
export HOLIAO_CONFIG="$W/holiao.yaml"
check "$ROOT/bin/holiao" login $H -u admin -p admin123
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
