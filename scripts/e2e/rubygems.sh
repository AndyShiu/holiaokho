set -e; cd /work
G=http://host.docker.internal:18081/repository/gems-group/
echo "== gem install via group (compact index + proxy)"
gem install --clear-sources --source $G --no-document rake -v 13.2.1 2>&1 | tail -1
echo "== build a gem and push to hosted"
mkdir -p hlgem/lib && cd hlgem && echo 'module Hlgem; V="1.0.0"; end' > lib/hlgem.rb
cat > hlgem.gemspec <<'X'
Gem::Specification.new do |s|
  s.name="hlgem"; s.version="1.0.0"; s.summary="e2e"; s.authors=["hl"]; s.files=["lib/hlgem.rb"]; s.license="MIT"
  s.add_runtime_dependency "rake", ">= 13.0"; s.required_ruby_version=">= 3.0"
end
X
gem build hlgem.gemspec 2>&1 | grep -E "Successfully|ERROR"
gem push hlgem-1.0.0.gem --host http://host.docker.internal:18081/repository/gems-hosted --key hl 2>&1 | tail -1 || true
# gem push needs the key in credentials file
mkdir -p ~/.gem && printf -- "---\n:hl: $HL_TOKEN\n" > ~/.gem/credentials && chmod 600 ~/.gem/credentials
gem push hlgem-1.0.0.gem --host http://host.docker.internal:18081/repository/gems-hosted --key hl 2>&1 | tail -1
echo "== duplicate push (allow_once)"; gem push hlgem-1.0.0.gem --host http://host.docker.internal:18081/repository/gems-hosted --key hl 2>&1 | tail -1
cd /work && echo "== info doc"; ruby -ropen-uri -e "puts URI.open(\"${G}info/hlgem\").read"
echo "== install own gem via group (pulls rake dep through proxy)"
gem install --clear-sources --source $G --no-document hlgem 2>&1 | tail -1
ruby -e 'require "hlgem"; puts "hlgem loaded " + Hlgem::V'
echo "== bundler"; mkdir -p bapp && cd bapp && printf "source '$G'\ngem 'hlgem'\ngem 'colorize', '1.1.0'\n" > Gemfile
bundle install --quiet 2>&1 | tail -2; bundle list | grep -E "hlgem|colorize|rake"
