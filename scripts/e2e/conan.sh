set -e; cd /work; export CONAN_HOME=/work/.conan2
pip install -q conan 2>&1 | grep -v notice | tail -1
conan profile detect -f >/dev/null 2>&1
conan remote remove '*' >/dev/null 2>&1 || true
conan remote add hl http://host.docker.internal:18081/repository/conan-group --insecure >/dev/null
conan remote login hl admin -p admin123 2>&1 | tail -1
echo "== install zlib via group (recipe from conancenter proxy)"
conan install --requires=zlib/1.3.1 -r hl --build=missing 2>&1 | grep -E "zlib/1.3.1.*(Cache|Download|Build)|ERROR" | head -3
echo "== create own package and upload to hosted (via group forwarding → hosted)"
mkdir -p hlpkg && cd hlpkg && conan new cmake_lib -d name=hlpkg -d version=0.1 -f >/dev/null 2>&1
conan create . -r hl --build=missing 2>&1 | grep -E "hlpkg/0.1.*(Created|Build)|ERROR" | head -2
conan upload 'hlpkg/*' -r hl -c 2>&1 | grep -E "Uploading|ERROR|Upload" | head -4
echo "== fresh cache: install own package from remote"
conan remove '*' -c >/dev/null 2>&1
conan install --requires=hlpkg/0.1 -r hl 2>&1 | grep -E "hlpkg/0.1.*(Download|Cache)|ERROR" | head -2
conan list 'hlpkg/*' -r hl 2>&1 | grep -E "hlpkg|ERROR" | head -2
