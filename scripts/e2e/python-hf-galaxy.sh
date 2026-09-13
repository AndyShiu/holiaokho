set -e; cd /work
pip install -q huggingface_hub ansible-core 2>&1 | tail -1
echo "== huggingface_hub: 從我們的 proxy 下載"
HF_ENDPOINT=http://host.docker.internal:18081/repository/hf HF_HUB_CACHE=/work/hfcache python3 -c "
from huggingface_hub import hf_hub_download, HfApi
p = hf_hub_download('hf-internal-testing/tiny-random-bert', 'config.json'); print('downloaded', open(p).read()[:60].replace(chr(10),' '))
info = HfApi().model_info('hf-internal-testing/tiny-random-bert'); print('model_info sha', info.sha[:12], 'files', len(info.siblings))
"
echo "== ansible-galaxy: 從 group 安裝 community.general 的小依賴（ansible.utils 較小）"
cat > ansible.cfg <<X
[galaxy]
server_list = hl
[galaxy_server.hl]
url=http://host.docker.internal:18081/repository/galaxy-group/api/
X
ansible-galaxy collection install ansible.utils:5.1.2 -p /work/collections 2>&1 | grep -E "Installing|installed|ERROR" | head -3
echo "== build + publish own collection to hosted"
mkdir -p src && cd src && ansible-galaxy collection init hl.demo >/dev/null && cd hl/demo && ansible-galaxy collection build >/dev/null && ls *.tar.gz
cat > /work/ansible2.cfg <<X
[galaxy]
server_list = hosted
[galaxy_server.hosted]
url=http://host.docker.internal:18081/repository/galaxy-hosted/api/
token=$HL_TOKEN
X
ANSIBLE_CONFIG=/work/ansible2.cfg ansible-galaxy collection publish hl-demo-1.0.0.tar.gz 2>&1 | grep -E "Collection has been|published|ERROR" | head -2
cd /work && ansible-galaxy collection install hl.demo:1.0.0 -p /work/collections 2>&1 | grep -E "installed|ERROR" | head -2
ls /work/collections/ansible_collections/hl/demo | head -3
