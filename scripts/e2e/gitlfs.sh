set -e; cd /work
git config --global user.email t@t; git config --global user.name t; git config --global init.defaultBranch main
git config --global lfs.url http://admin:admin123@host.docker.internal:18081/repository/lfs
git init -q bare.git --bare; git init -q repo && cd repo && git remote add origin /work/bare.git
git lfs install --local >/dev/null; git lfs track "*.bin" >/dev/null
head -c 300000 /dev/urandom > big.bin; git add .gitattributes big.bin && git commit -qm "lfs"
git push -q origin main 2>&1 | tail -1; echo "pushed"
cd /work && git clone -q /work/bare.git clone && cd clone
cmp big.bin ../repo/big.bin && echo "LFS object round-trip OK ($(stat -c %s big.bin) bytes)"
git lfs ls-files | head -1
echo "== lock API"; git lfs lock big.bin 2>&1 | tail -1; git lfs locks 2>&1 | tail -1
