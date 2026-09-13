set -e; cd /work; export PUB_CACHE=/work/.pub
cp /work/ca.crt /usr/local/share/ca-certificates/hl.crt && update-ca-certificates >/dev/null 2>&1
HOSTED=https://host.docker.internal:18443/repository/pub-hosted
echo "== create package, publish to hosted (https)"
rm -rf hlpub app; dart create -t package hlpub --no-pub >/dev/null && cd hlpub && echo MIT > LICENSE
sed -i 's/^version: .*/version: 1.0.0/' pubspec.yaml; printf "publish_to: %s\n" "$HOSTED" >> pubspec.yaml
dart pub token add "$HOSTED" --env-var HL_TOKEN >/dev/null
dart pub publish --force 2>&1 | grep -E "Successfully|rror|uploaded" | head -2
echo "== duplicate publish"; dart pub publish --force 2>&1 | grep -iE "already|rror" | head -1
echo "== consume via group: own package + pub.dev dep"
cd /work && dart create -t console app --no-pub >/dev/null && cd app
export PUB_HOSTED_URL=https://host.docker.internal:18443/repository/pub-group
dart pub add hlpub:1.0.0 'path:^1.9.0' 2>&1 | grep -E "^\+|error" | head -3
grep -A3 "hlpub:" pubspec.lock | grep url
