set -e; cd /work; export COMPOSER_HOME=/work/.composer COMPOSER_ALLOW_SUPERUSER=1
G=http://host.docker.internal:18081/repository/composer-group
mkdir -p app && cd app
cat > composer.json <<X
{"name":"hl/app","config":{"secure-http":false},"repositories":[{"type":"composer","url":"$G"},{"packagist.org":false}],"require":{}}
X
echo "== require monolog via group"
composer require --quiet --no-interaction monolog/monolog:3.7.0 2>&1 | tail -2
grep -o '"url": "[^"]*monolog[^"]*"' composer.lock | head -1
php -r 'require "vendor/autoload.php"; echo "monolog class ok: ", class_exists("Monolog\\Logger") ? "yes":"no", "\n";'
echo "== upload own package through the group (lands in composer-hosted)"
mkdir -p /work/hlpkg/src && cd /work/hlpkg
printf '{"name":"hl/hlpkg","version":"1.0.0","autoload":{"psr-4":{"Hl\\\\":"src/"}}}' > composer.json
printf '<?php\nnamespace Hl;\nclass Hello { public static function hi() { return "hi from hl/hlpkg"; } }\n' > src/Hello.php
zip -q -r /work/hlpkg.zip composer.json src
curl -s -o /dev/null -w "upload %{http_code}\n" -u "admin:$HL_TOKEN" --upload-file /work/hlpkg.zip "$G/packages/upload/hl/hlpkg/1.0.0"
cd /work/app
echo "== require own package via group"
composer require --quiet --no-interaction hl/hlpkg:1.0.0 2>&1 | tail -2
grep -o '"url": "[^"]*hlpkg[^"]*"' composer.lock | head -1
php -r 'require "vendor/autoload.php"; echo Hl\Hello::hi(), "\n";'
