set -e; cd /work
echo "== repo add + update (group → bitnami index via proxy)"
helm repo add hl http://host.docker.internal:18081/repository/helm-group/ >/dev/null && helm repo update >/dev/null && echo ok
echo "== search"; helm search repo hl/nginx --versions 2>/dev/null | head -3
echo "== pull chart via group"; helm pull hl/nginx --version 18.2.0 && ls -la nginx-18.2.0.tgz | awk '{print $5, $9}'
echo "== create + package own chart"; helm create mychart >/dev/null && helm package mychart >/dev/null && ls mychart-0.1.0.tgz
echo "== upload to hosted (curl, ChartMuseum API)"
wget -q -O - --post-file=mychart-0.1.0.tgz --header="Authorization: Basic YWRtaW46YWRtaW4xMjM=" http://host.docker.internal:18081/repository/helm-hosted/api/charts; echo
echo "== hosted index"; wget -q -O - http://host.docker.internal:18081/repository/helm-hosted/index.yaml | grep -E "mychart|version:|urls" | head -4
echo "== install own chart via group (dry-run, client-only)"
helm repo update >/dev/null && helm search repo hl/mychart && helm pull hl/mychart --version 0.1.0 -d out && ls out
helm template t hl/mychart --version 0.1.0 | grep -c "kind:"
