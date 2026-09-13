#!/usr/bin/env bash
# Per-format end-to-end tests with real clients, each in its official Docker
# image, against a running dev server on :18081 (+ :18443 TLS for Terraform/pub).
#   scripts/e2e-formats.sh              run every format
#   scripts/e2e-formats.sh nuget helm   run selected formats
# Prereqs: scripts/dev-restart.sh running; docker; scratchpad TLS certs for terraform/pub
# (see scripts/dev-restart.sh). Test containers pull their images on first use.
set -uo pipefail
cd "$(dirname "$0")/.."
H=http://localhost:18081
api() { curl -sf -u admin:admin123 -H 'Content-Type: application/json' "$@"; }
mk()  { api -X POST $H/api/v1/repositories -d "$1" >/dev/null 2>&1 || true; }
run() { echo; echo "===== $1"; shift; "$@"; }
export HL_TOKEN=$(api -X POST $H/api/v1/me/tokens -d '{"name":"e2e-formats"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['secret'])")
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
want="${*:-nuget helm go apt yum alpine rubygems cargo composer conda cran pypi terraform pub gitlfs huggingface ansible conan}"
D=scripts/e2e

for f in $want; do case $f in
nuget)
  mk '{"name":"nuget.org-proxy","format":"nuget","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://api.nuget.org/v3/index.json"}}}'
  mk '{"name":"nuget-hosted","format":"nuget","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
  mk '{"name":"nuget-group","format":"nuget","type":"group","attributes":{"group":{"members":["nuget-hosted","nuget.org-proxy"]}}}'
  mkdir -p $T/nuget && cp $D/nuget.sh $T/nuget/run.sh
  run nuget docker run --rm -e HL_TOKEN -v $T/nuget:/work -w /work mcr.microsoft.com/dotnet/sdk:8.0 sh /work/run.sh ;;
helm)
  mk '{"name":"helm-bitnami","format":"helm","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://charts.bitnami.com/bitnami"}}}'
  mk '{"name":"helm-hosted","format":"helm","type":"hosted"}'
  mk '{"name":"helm-group","format":"helm","type":"group","attributes":{"group":{"members":["helm-hosted","helm-bitnami"]}}}'
  mkdir -p $T/helm && cp $D/helm.sh $T/helm/run.sh
  run helm docker run --rm -v $T/helm:/work -w /work --entrypoint sh alpine/helm:3.16.2 /work/run.sh ;;
go)
  mk '{"name":"go-proxy","format":"go","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://proxy.golang.org"}}}'
  mk '{"name":"go-group","format":"go","type":"group","attributes":{"group":{"members":["go-proxy"]}}}'
  mkdir -p $T/go && printf 'module e2e\ngo 1.22\nrequire github.com/google/uuid v1.6.0\n' > $T/go/go.mod && echo 'package main; import ("fmt"; "github.com/google/uuid"); func main(){ fmt.Println(uuid.NewString()) }' > $T/go/main.go
  run go docker run --rm -v $T/go:/work -w /work -e GOPROXY=http://host.docker.internal:18081/repository/go-group -e GOSUMDB=sum.golang.org -e GOFLAGS=-mod=mod golang:1.23-alpine sh -c "go mod tidy && go run . && echo GO OK" ;;
apt)
  api -X POST $H/api/v1/system/pgp-key -d '{}' > $T/pgp.json
  python3 - "$T" <<'PY'
import json,sys; t=sys.argv[1]; k=json.load(open(t+'/pgp.json'))
json.dump({"name":"apt-hosted","format":"apt","type":"hosted","attributes":{"apt":{"distribution":"stable","component":"main","signingKey":k['privateKey']}}}, open(t+'/apt.json','w'))
json.dump({"name":"yum-hosted","format":"yum","type":"hosted","attributes":{"yum":{"signingKey":k['privateKey']}}}, open(t+'/yum.json','w'))
open(t+'/hl.pub','w').write(k['publicKey'])
PY
  mk "$(cat $T/apt.json)"; mk '{"name":"debian","format":"apt","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://deb.debian.org/debian"}}}'
  curl -sf -o $T/hello.deb $H/repository/debian/pool/main/h/hello/hello_2.10-3_arm64.deb && curl -sf -u admin:admin123 -X POST --data-binary @$T/hello.deb -o /dev/null $H/repository/apt-hosted/
  mkdir -p $T/apt && cp $D/apt.sh $T/apt/run.sh && cp $T/hl.pub $T/apt/
  run apt docker run --rm --platform linux/arm64 -v $T/apt:/work debian:bookworm-slim bash /work/run.sh ;;
yum)
  [ -f $T/yum.json ] || { api -X POST $H/api/v1/system/pgp-key -d '{}' > $T/pgp.json; python3 -c "
import json; k=json.load(open('$T/pgp.json')); json.dump({'name':'yum-hosted','format':'yum','type':'hosted','attributes':{'yum':{'signingKey':k['privateKey']}}}, open('$T/yum.json','w')); open('$T/hl.pub','w').write(k['publicKey'])"; }
  mk "$(cat $T/yum.json)"; mk '{"name":"rocky","format":"yum","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://dl.rockylinux.org/pub/rocky"}}}'
  curl -sf -o $T/pkg.rpm "$H/repository/rocky/9/BaseOS/aarch64/os/Packages/z/zlib-1.2.11-40.el9.aarch64.rpm" && curl -sf -u admin:admin123 -X POST --data-binary @$T/pkg.rpm -o /dev/null $H/repository/yum-hosted/
  mkdir -p $T/yum && cp $D/yum.sh $T/yum/run.sh && cp $T/hl.pub $T/yum/
  run yum docker run --rm --platform linux/arm64 -v $T/yum:/work rockylinux:9 bash /work/run.sh ;;
alpine)
  api -X POST $H/api/v1/system/rsa-key > $T/rsa.json
  python3 -c "
import json; k=json.load(open('$T/rsa.json')); json.dump({'name':'apk-hosted','format':'alpine','type':'hosted','attributes':{'alpine':{'signingKey':k['privateKey'],'keyName':'hl.rsa.pub'}}}, open('$T/apk.json','w')); open('$T/hl.rsa.pub','w').write(k['publicKey'])"
  mk "$(cat $T/apk.json)"; mk '{"name":"alpine","format":"alpine","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://dl-cdn.alpinelinux.org/alpine"}}}'
  curl -sf -o $T/pkg.apk "$H/repository/alpine/v3.20/main/aarch64/tree-2.1.1-r0.apk" && curl -sf -u admin:admin123 -X PUT --data-binary @$T/pkg.apk -o /dev/null "$H/repository/apk-hosted/aarch64/tree-2.1.1-r0.apk"
  mkdir -p $T/apk && cp $D/alpine.sh $T/apk/run.sh && cp $T/hl.rsa.pub $T/apk/
  run alpine docker run --rm --platform linux/arm64 -v $T/apk:/work alpine:3.20 sh /work/run.sh ;;
rubygems)
  mk '{"name":"gems-proxy","format":"rubygems","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://rubygems.org"}}}'
  mk '{"name":"gems-hosted","format":"rubygems","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
  mk '{"name":"gems-group","format":"rubygems","type":"group","attributes":{"group":{"members":["gems-hosted","gems-proxy"]}}}'
  mkdir -p $T/gem && cp $D/rubygems.sh $T/gem/run.sh
  run rubygems docker run --rm -e HL_TOKEN -v $T/gem:/work -w /work ruby:3.3-slim bash /work/run.sh ;;
cargo)
  mk '{"name":"crates-proxy","format":"cargo","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://index.crates.io"}}}'
  mk '{"name":"crates-hosted","format":"cargo","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
  mk '{"name":"crates-group","format":"cargo","type":"group","attributes":{"group":{"members":["crates-hosted","crates-proxy"]}}}'
  mkdir -p $T/cargo && cp $D/cargo.sh $T/cargo/run.sh
  run cargo docker run --rm -e HL_TOKEN -v $T/cargo:/work -w /work rust:1.82-slim bash /work/run.sh ;;
composer)
  mk '{"name":"packagist","format":"composer","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://repo.packagist.org"}}}'
  mk '{"name":"composer-hosted","format":"composer","type":"hosted"}'
  mk '{"name":"composer-group","format":"composer","type":"group","attributes":{"group":{"members":["composer-hosted","packagist"]}}}'
  mkdir -p $T/composer && cp $D/composer.sh $T/composer/run.sh
  run composer docker run --rm -v $T/composer:/work -w /work composer:2 sh -c "apk add -q zip php83-zip >/dev/null 2>&1; sh /work/run.sh" ;;
conda)
  mk '{"name":"conda-forge","format":"conda","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://conda.anaconda.org/conda-forge"}}}'
  mk '{"name":"conda-hosted","format":"conda","type":"hosted"}'
  run conda docker run --rm --platform linux/arm64 mambaorg/micromamba:1.5.10 bash -c "micromamba create -y -q -n u -c http://host.docker.internal:18081/repository/conda-forge --override-channels ca-certificates >/dev/null && micromamba list -n u | grep -c ca-certificates && echo CONDA OK" ;;
cran)
  mk '{"name":"cran","format":"r","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://cloud.r-project.org"}}}'
  run cran docker run --rm --platform linux/arm64 r-base:4.4.1 Rscript -e 'install.packages("R6", repos="http://host.docker.internal:18081/repository/cran", quiet=TRUE); library(R6); cat("CRAN OK\n")' ;;
pypi)
  mk '{"name":"pypi-proxy","format":"pypi","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://pypi.org/"}}}'
  mk '{"name":"pypi-group","format":"pypi","type":"group","attributes":{"group":{"members":["pypi-proxy"]}}}'
  run pypi docker run --rm python:3.12-slim sh -c "pip install -q --index-url http://host.docker.internal:18081/repository/pypi-group/simple/ --trusted-host host.docker.internal six && echo PYPI OK" ;;
terraform)
  mk '{"name":"tf-registry","format":"terraform","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://registry.terraform.io"}}}'
  mk '{"name":"tf-hosted","format":"terraform","type":"hosted"}'
  mk '{"name":"tf-group","format":"terraform","type":"group","attributes":{"group":{"members":["tf-hosted","tf-registry"]}}}'
  mkdir -p $T/tf/mod && printf 'output "hello" { value = "hi from hosted module" }\n' > $T/tf/mod/main.tf && (cd $T/tf/mod && COPYFILE_DISABLE=1 tar czf ../mod.tgz main.tf)
  curl -sf -u admin:admin123 -X PUT --data-binary @$T/tf/mod.tgz -o /dev/null $H/repository/tf-hosted/modules/hl/hello/null/1.0.0.tgz || true
  cp $D/terraform-main.tf $T/tf/main.tf; cp $D/terraformrc $T/tf/terraformrc; cp "${TLS_DIR:-/private/tmp/claude-501/-Users-andyshiu-work-05-private-holiao/56d79738-2b23-42e7-afa5-8b3d145456ed/scratchpad/tls}/ca.crt" $T/tf/ca.crt 2>/dev/null || echo "(no TLS CA; terraform test needs HTTPS)"
  run terraform docker run --rm -v $T/tf:/work -w /work -e TF_CLI_CONFIG_FILE=/work/terraformrc --entrypoint sh hashicorp/terraform:1.9 -c "cp /work/ca.crt /usr/local/share/ca-certificates/hl.crt && update-ca-certificates >/dev/null 2>&1; terraform init -no-color | grep -E 'Installed|Error'; terraform apply -auto-approve -no-color | grep -E 'hello =|Error'" ;;
pub)
  mk '{"name":"pub-dev","format":"pub","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://pub.dev"}}}'
  mk '{"name":"pub-hosted","format":"pub","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
  mk '{"name":"pub-group","format":"pub","type":"group","attributes":{"group":{"members":["pub-hosted","pub-dev"]}}}'
  mkdir -p $T/pub && cp $D/pub.sh $T/pub/run.sh && cp "${TLS_DIR:-/private/tmp/claude-501/-Users-andyshiu-work-05-private-holiao/56d79738-2b23-42e7-afa5-8b3d145456ed/scratchpad/tls}/ca.crt" $T/pub/ 2>/dev/null
  run pub docker run --rm -e HL_TOKEN -v $T/pub:/work -w /work dart:stable bash /work/run.sh ;;
gitlfs)
  mk '{"name":"lfs","format":"gitlfs","type":"hosted"}'
  mkdir -p $T/lfs && cp $D/gitlfs.sh $T/lfs/run.sh
  run gitlfs docker run --rm -v $T/lfs:/work -w /work alpine:3.20 sh -c "apk add -q git git-lfs >/dev/null && sh /work/run.sh" ;;
huggingface|ansible)
  mk '{"name":"hf","format":"huggingface","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://huggingface.co"}}}'
  mk '{"name":"galaxy","format":"ansiblegalaxy","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://galaxy.ansible.com"}}}'
  mk '{"name":"galaxy-hosted","format":"ansiblegalaxy","type":"hosted","attributes":{"hosted":{"writePolicy":"allow_once"}}}'
  mk '{"name":"galaxy-group","format":"ansiblegalaxy","type":"group","attributes":{"group":{"members":["galaxy-hosted","galaxy"]}}}'
  mkdir -p $T/py && cp $D/python-hf-galaxy.sh $T/py/run.sh
  run "huggingface+ansible" docker run --rm -e HL_TOKEN -v $T/py:/work -w /work python:3.12-slim bash /work/run.sh ;;
conan)
  mk '{"name":"conancenter","format":"conan","type":"proxy","attributes":{"proxy":{"remoteUrl":"https://center2.conan.io"}}}'
  mk '{"name":"conan-hosted","format":"conan","type":"hosted"}'
  mk '{"name":"conan-group","format":"conan","type":"group","attributes":{"group":{"members":["conan-hosted","conancenter"]}}}'
  mkdir -p $T/conan && cp $D/conan.sh $T/conan/run.sh
  run conan docker run --rm -v $T/conan:/work -w /work python:3.12-slim bash -c "apt-get update -qq >/dev/null && apt-get install -y -qq cmake g++ make >/dev/null 2>&1; bash /work/run.sh" ;;
esac; done
