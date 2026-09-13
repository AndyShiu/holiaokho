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
echo "== require own package via group"
composer require --quiet --no-interaction hl/hlpkg:1.0.0 2>&1 | tail -2
grep -o '"url": "[^"]*hlpkg[^"]*"' composer.lock | head -1
php -r 'require "vendor/autoload.php"; echo Hl\Hello::hi(), "\n";'
