set -e
export DEBIAN_FRONTEND=noninteractive
echo "== 1. base archive via our proxy"
cat > /etc/apt/sources.list.d/debian.sources <<X
Types: deb
URIs: http://host.docker.internal:18081/repository/debian
Suites: bookworm bookworm-updates
Components: main
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
X
rm -f /etc/apt/sources.list /etc/apt/sources.list.d/debian.list 2>/dev/null || true
apt-get update 2>&1 | grep -E "^(Get|Hit|Err|E:)" | head -4
echo "== 2. hosted signed repo"
# apt accepts ASCII-armored keys in signed-by directly (apt >= 2.4)
cp /work/hl.pub /etc/apt/keyrings-hl.asc
echo "deb [signed-by=/etc/apt/keyrings-hl.asc] http://host.docker.internal:18081/repository/apt-hosted stable main" > /etc/apt/sources.list.d/hl.list
apt-get update 2>&1 | grep -E "apt-hosted|E:|W:" | head -3
echo "== 3. install hello from hosted (deps from proxy)"
apt-get install -y hello 2>&1 | grep -E "^Get:|hello" | head -4
hello
apt-cache policy hello | grep -A1 "\*\*\*"
