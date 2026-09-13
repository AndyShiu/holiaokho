set -e
echo "== 1. BaseOS via our proxy"
sed -i 's/^mirrorlist=/#mirrorlist=/; s|^#baseurl=http://dl.rockylinux.org/$contentdir|baseurl=http://host.docker.internal:18081/repository/rocky|' /etc/yum.repos.d/rocky.repo
dnf -q makecache 2>&1 | tail -2; echo "makecache ok"
echo "== 2. hosted signed repo"
cat > /etc/yum.repos.d/hl.repo <<X
[hl]
name=Holiaokho hosted
baseurl=http://host.docker.internal:18081/repository/yum-hosted/
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=file:///work/hl.pub
X
dnf -q -y --disablerepo='*' --enablerepo=hl repolist 2>&1 | tail -2
dnf -q -y --disablerepo='*' --enablerepo=hl repoquery --queryformat '%{name}-%{version}-%{release}.%{arch} from %{repoid}\n' zlib 2>&1 | tail -1
echo "== 3. install from proxy"
dnf -q install -y --disablerepo=hl tree 2>&1 | tail -1; tree --version | head -1
echo "== 4. reinstall zlib from hosted (repo_gpgcheck verifies repomd.xml.asc)"
dnf -q reinstall -y --disablerepo='*' --enablerepo=hl zlib 2>&1 | tail -2; rpm -q zlib
