set -e
echo "== 1. main/community via our proxy"
printf 'http://host.docker.internal:18081/repository/alpine/v3.20/main\nhttp://host.docker.internal:18081/repository/alpine/v3.20/community\n' > /etc/apk/repositories
apk update 2>&1 | grep -E "OK|ERROR|WARN" | head -3
echo "== 2. hosted signed repo"
cp /work/hl.rsa.pub /etc/apk/keys/hl.rsa.pub
echo "http://host.docker.internal:18081/repository/apk-hosted" >> /etc/apk/repositories
apk update 2>&1 | grep -E "apk-hosted|OK|ERROR|WARN|UNTRUSTED" | head -3
echo "== 3. install tree from hosted"
apk add --no-cache tree 2>&1 | grep -E "Installing|ERROR|OK"
apk info tree | head -1; tree --version | head -1
apk policy tree 2>/dev/null | head -4
