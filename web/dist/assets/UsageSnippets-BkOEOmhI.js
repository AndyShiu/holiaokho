import{u as h,j as d,L as $}from"./react-BWHAFyLI.js";import{a as f}from"./Copyable-S_apjA7b.js";function k(i,o=window.location.origin){return i.url||`${o}/repository/${i.name}`}function b(i){try{return new URL(i).host}catch{return i}}function E(i,o){var u,n,g;const e=k(i),a=b(e),t=i.name,p=i.type==="hosted",r=[],m=o("usage.credNote","# credentials: your username + a User Token as the password");switch(i.format){case"maven":r.push({title:o("usage.maven.mirror","Use as mirror"),file:"~/.m2/settings.xml",code:`<settings>
  <mirrors>
    <mirror>
      <id>${t}</id>
      <mirrorOf>*</mirrorOf>
      <url>${e}/</url>
    </mirror>
  </mirrors>
  <servers>
    <server>
      <id>${t}</id>
      <username>USERNAME</username>
      <password>USER_TOKEN</password>
    </server>
  </servers>
</settings>`}),p&&r.push({title:o("usage.maven.deploy","Deploy"),file:"pom.xml",code:`<distributionManagement>
  <repository>
    <id>${t}</id>
    <url>${e}/</url>
  </repository>
  <snapshotRepository>
    <id>${t}</id>
    <url>${e}/</url>
  </snapshotRepository>
</distributionManagement>`}),r.push({title:"Gradle",file:"build.gradle.kts",code:`repositories {
    maven {
        url = uri("${e}/")
        credentials { username = "USERNAME"; password = "USER_TOKEN" }
    }
}`});break;case"npm":r.push({title:o("usage.npm.registry","Registry"),file:".npmrc",code:`registry=${e}/
//${a}/repository/${t}/:_authToken=USER_TOKEN`}),r.push({title:o("usage.npm.scope","Only for a scope"),file:".npmrc",code:`@myorg:registry=${e}/`}),p&&r.push({title:o("usage.npm.publish","Publish"),code:`npm publish --registry ${e}/`});break;case"docker":{const s=i.attributes.docker??{},c=a.split(":")[0];if(s.httpPort&&r.push({title:o("usage.docker.port","Port connector"),desc:`${c}:${s.httpPort}`,code:`docker login ${c}:${s.httpPort} -u USERNAME -p USER_TOKEN
docker pull ${c}:${s.httpPort}/library/alpine:3.20
${p?`docker tag myimage ${c}:${s.httpPort}/myimage:1.0
docker push ${c}:${s.httpPort}/myimage:1.0`:""}`.trim()}),s.httpsPort&&r.push({title:o("usage.docker.tls","TLS port connector"),code:`docker login ${c}:${s.httpsPort} -u USERNAME -p USER_TOKEN
docker pull ${c}:${s.httpsPort}/library/alpine:3.20`}),s.pathEnabled!==!1&&r.push({title:o("usage.docker.path","Path mode (main port)"),desc:`${a}/${t}/<image>`,code:`docker login ${a} -u USERNAME -p USER_TOKEN
docker pull ${a}/${t}/library/alpine:3.20${p?`
docker push ${a}/${t}/myimage:1.0`:""}`}),s.subdomain&&r.push({title:o("usage.docker.subdomain","Subdomain"),code:`docker pull ${s.subdomain}.${a}/library/alpine:3.20`}),i.type!=="hosted"){const l=s.httpPort?`http://${c}:${s.httpPort}`:s.httpsPort?`https://${c}:${s.httpsPort}`:s.subdomain?`https://${s.subdomain}.${a}`:"";r.push({title:o("usage.docker.mirror","Docker daemon registry mirror"),file:l?"/etc/docker/daemon.json":void 0,desc:l?void 0:o("usage.docker.mirrorNeedsPort","Needs a dedicated port or subdomain"),code:l?`{
  "registry-mirrors": ["${l}"]
}`:o("usage.docker.mirrorHint",`# Docker only accepts scheme://host:port here — a path such as
# /repository/<name> is ignored. Give this repository an HTTP/HTTPS
# port or a subdomain in Settings, then this snippet appears.`),wide:!0})}break}case"pypi":r.push({title:"pip",file:"~/.config/pip/pip.conf",code:`[global]
index-url = ${e}/simple
# with credentials:
# index-url = https://USERNAME:USER_TOKEN@${a}/repository/${t}/simple`}),p&&r.push({title:"twine",file:"~/.pypirc",code:`[distutils]
index-servers = ${t}

[${t}]
repository = ${e}/
username = USERNAME
password = USER_TOKEN

# twine upload -r ${t} dist/*`});break;case"raw":r.push({title:o("usage.raw.download","Download"),code:`curl -O ${e}/path/to/file.txt`}),p&&r.push({title:o("usage.raw.upload","Upload"),code:`curl -u USERNAME:USER_TOKEN --upload-file ./file.txt ${e}/path/to/file.txt`});break;case"nuget":r.push({title:"dotnet",code:`dotnet nuget add source ${e}/index.json -n ${t} -u USERNAME -p USER_TOKEN --store-password-in-clear-text`}),p&&r.push({title:o("usage.nuget.push","Push"),code:`dotnet nuget push MyPkg.1.0.0.nupkg -s ${t} -k USER_TOKEN`});break;case"helm":r.push({title:"helm repo add",code:`helm repo add ${t} ${e}/ --username USERNAME --password USER_TOKEN
helm repo update`}),p&&r.push({title:o("usage.helm.push","Upload a chart"),code:`curl -u USERNAME:USER_TOKEN --upload-file mychart-1.0.0.tgz ${e}/`});break;case"go":r.push({title:"GOPROXY",code:`export GOPROXY=${e}
export GONOSUMDB=
export GOSUMDB="sum.golang.org ${e}/sumdb"`,wide:!0});break;case"apt":r.push({title:o("usage.apt.sources","Sources"),file:"/etc/apt/sources.list.d/holiaokho.list",code:`curl -fsSL ${e}/repository-key.gpg | gpg --dearmor -o /etc/apt/keyrings/${t}.gpg
echo "deb [signed-by=/etc/apt/keyrings/${t}.gpg] ${e} ${((u=i.attributes.apt)==null?void 0:u.distribution)??"stable"} ${((n=i.attributes.apt)==null?void 0:n.component)??"main"}" > /etc/apt/sources.list.d/${t}.list
apt-get update`,wide:!0}),p&&r.push({title:o("usage.apt.upload","Upload a .deb"),code:`curl -u USERNAME:USER_TOKEN --upload-file mypkg_1.0_amd64.deb ${e}/`});break;case"yum":r.push({title:".repo",file:`/etc/yum.repos.d/${t}.repo`,code:`[${t}]
name=${t}
baseurl=${e}/
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=${e}/repository-key.gpg`}),p&&r.push({title:o("usage.yum.upload","Upload an .rpm"),code:`curl -u USERNAME:USER_TOKEN --upload-file mypkg-1.0-1.x86_64.rpm ${e}/`});break;case"alpine":{const s=((g=i.attributes.alpine)==null?void 0:g.keyName)??"holiaokho.rsa.pub";r.push({title:"apk",file:"/etc/apk/repositories",code:`curl -fsSL ${e}/${s} -o /etc/apk/keys/${s}
echo "${e}/main" >> /etc/apk/repositories
apk update`,wide:!0});break}case"rubygems":r.push({title:"gem",code:`gem sources --add ${e}/
# or in Gemfile:
source "${e}/"`}),p&&r.push({title:"gem push",code:`gem push --host ${e} mygem-1.0.0.gem`});break;case"cargo":r.push({title:"cargo",file:".cargo/config.toml",code:`[registries]
${t} = { index = "sparse+${e}/" }

[net]
git-fetch-with-cli = true`}),r.push({title:"cargo login",code:`cargo login --registry ${t} "Bearer USER_TOKEN"${p?`
cargo publish --registry ${t}`:""}`});break;case"composer":r.push({title:"composer",code:`composer config repositories.${t} composer ${e}/
composer config http-basic.${a} USERNAME USER_TOKEN`});break;case"conda":r.push({title:"conda",file:"~/.condarc",code:`channels:
  - ${e}/main
  - ${e}/conda-forge`});break;case"cran":r.push({title:"R",file:"~/.Rprofile",code:`options(repos = c(CRAN = "${e}/"))`});break;case"p2":r.push({title:"Eclipse update site",code:`${e}/`});break;case"cocoapods":r.push({title:"Podfile",file:"Podfile",code:`source '${e}/'`});break;case"terraform":r.push({title:o("usage.tf.mirror","Provider network mirror"),file:"~/.terraformrc",code:`provider_installation {
  network_mirror {
    url = "${e}/providers/"
  }
}`}),r.push({title:o("usage.tf.module","Module source"),code:`module "vpc" {
  source  = "${a}/namespace/name/provider"
  version = "1.0.0"
}`});break;case"pub":r.push({title:"pub",code:`export PUB_HOSTED_URL=${e}
dart pub get`}),p&&r.push({title:"dart pub publish",code:`dart pub token add ${e}
dart pub publish`});break;case"gitlfs":r.push({title:"Git LFS",file:".lfsconfig",code:`[lfs]
	url = ${e}/`});break;case"huggingface":r.push({title:"huggingface_hub",code:`export HF_ENDPOINT=${e}`});break;case"ansiblegalaxy":r.push({title:"ansible-galaxy",file:"ansible.cfg",code:`[galaxy]
server_list = ${t}

[galaxy_server.${t}]
url=${e}/
token=USER_TOKEN`});break;case"conan":r.push({title:"conan",code:`conan remote add ${t} ${e}
conan remote login ${t} USERNAME -p USER_TOKEN${p?`
conan upload "*" -r ${t} --confirm`:""}`});break;case"swift":r.push({title:"swift package-registry",code:`swift package-registry set ${e}
swift package-registry login ${e} --username USERNAME --password USER_TOKEN`});break;default:r.push({title:"URL",code:e})}return r.map(s=>({...s,code:s.code.replace(/\n{3,}/g,`

`)})).concat([{title:"",code:m,wide:!0}]).filter(s=>s.title!=="")}function R({repo:i}){const{t:o}=h(),e=E(i,o);return d.jsxs("div",{children:[d.jsx("div",{style:{display:"grid",gridTemplateColumns:"repeat(auto-fit, minmax(380px, 1fr))",gap:16},children:e.map((a,t)=>d.jsx("div",{style:a.wide?{gridColumn:"1 / -1"}:void 0,children:d.jsx(f,{code:a.code,title:a.title,filename:a.file??a.desc})},t))}),d.jsxs("div",{style:{display:"flex",alignItems:"center",gap:10,marginTop:16,fontSize:13},children:[d.jsx("span",{style:{width:8,height:8,background:"var(--hlk-amber)",borderRadius:2}}),o("usage.cred","Credentials: use a User Token (username + token as the password)."),d.jsx($,{to:"/me/tokens",children:o("usage.createToken","Create my token →")})]})]})}export{R as U,k as r,E as s};
