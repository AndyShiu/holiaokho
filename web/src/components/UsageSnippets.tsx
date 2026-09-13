import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { Repository } from '@/api/types'
import { CodeBlock } from './Copyable'

interface Snip { title: string; desc?: string; file?: string; code: string; wide?: boolean }

export function repoBase(rp: Repository, origin = window.location.origin) {
  return rp.url || `${origin}/repository/${rp.name}`
}

function hostOf(url: string) {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

export function snippetsFor(rp: Repository, t: (k: string, d: string, o?: any) => string): Snip[] {
  const url = repoBase(rp)
  const host = hostOf(url)
  const n = rp.name
  const hosted = rp.type === 'hosted'
  const s: Snip[] = []
  const cred = t('usage.credNote', '# credentials: your username + a User Token as the password')
  switch (rp.format) {
    case 'maven':
      s.push({ title: t('usage.maven.mirror', 'Use as mirror'), file: '~/.m2/settings.xml', code: `<settings>\n  <mirrors>\n    <mirror>\n      <id>${n}</id>\n      <mirrorOf>*</mirrorOf>\n      <url>${url}/</url>\n    </mirror>\n  </mirrors>\n  <servers>\n    <server>\n      <id>${n}</id>\n      <username>USERNAME</username>\n      <password>USER_TOKEN</password>\n    </server>\n  </servers>\n</settings>` })
      if (hosted) s.push({ title: t('usage.maven.deploy', 'Deploy'), file: 'pom.xml', code: `<distributionManagement>\n  <repository>\n    <id>${n}</id>\n    <url>${url}/</url>\n  </repository>\n  <snapshotRepository>\n    <id>${n}</id>\n    <url>${url}/</url>\n  </snapshotRepository>\n</distributionManagement>` })
      s.push({ title: 'Gradle', file: 'build.gradle.kts', code: `repositories {\n    maven {\n        url = uri("${url}/")\n        credentials { username = "USERNAME"; password = "USER_TOKEN" }\n    }\n}` })
      break
    case 'npm':
      s.push({ title: t('usage.npm.registry', 'Registry'), file: '.npmrc', code: `registry=${url}/\n//${host}/repository/${n}/:_authToken=USER_TOKEN` })
      s.push({ title: t('usage.npm.scope', 'Only for a scope'), file: '.npmrc', code: `@myorg:registry=${url}/` })
      if (hosted) s.push({ title: t('usage.npm.publish', 'Publish'), code: `npm publish --registry ${url}/` })
      break
    case 'docker': {
      const d = rp.attributes.docker ?? {}
      const bareHost = host.split(':')[0]
      if (d.httpPort) s.push({ title: t('usage.docker.port', 'Port connector'), desc: `${bareHost}:${d.httpPort}`, code: `docker login ${bareHost}:${d.httpPort} -u USERNAME -p USER_TOKEN\ndocker pull ${bareHost}:${d.httpPort}/library/alpine:3.20\n${hosted ? `docker tag myimage ${bareHost}:${d.httpPort}/myimage:1.0\ndocker push ${bareHost}:${d.httpPort}/myimage:1.0` : ''}`.trim() })
      if (d.httpsPort) s.push({ title: t('usage.docker.tls', 'TLS port connector'), code: `docker login ${bareHost}:${d.httpsPort} -u USERNAME -p USER_TOKEN\ndocker pull ${bareHost}:${d.httpsPort}/library/alpine:3.20` })
      if (d.pathEnabled !== false) s.push({ title: t('usage.docker.path', 'Path mode (main port)'), desc: `${host}/${n}/<image>`, code: `docker login ${host} -u USERNAME -p USER_TOKEN\ndocker pull ${host}/${n}/library/alpine:3.20${hosted ? `\ndocker push ${host}/${n}/myimage:1.0` : ''}` })
      if (d.subdomain) s.push({ title: t('usage.docker.subdomain', 'Subdomain'), code: `docker pull ${d.subdomain}.${host}/library/alpine:3.20` })
      if (rp.type !== 'hosted') s.push({ title: t('usage.docker.mirror', 'Docker daemon registry mirror'), file: '/etc/docker/daemon.json', code: `{\n  "registry-mirrors": ["${d.httpPort ? `http://${bareHost}:${d.httpPort}` : `${url}`}"]\n}`, wide: true })
      break
    }
    case 'pypi':
      s.push({ title: 'pip', file: '~/.config/pip/pip.conf', code: `[global]\nindex-url = ${url}/simple\n# with credentials:\n# index-url = https://USERNAME:USER_TOKEN@${host}/repository/${n}/simple` })
      if (hosted) s.push({ title: 'twine', file: '~/.pypirc', code: `[distutils]\nindex-servers = ${n}\n\n[${n}]\nrepository = ${url}/\nusername = USERNAME\npassword = USER_TOKEN\n\n# twine upload -r ${n} dist/*` })
      break
    case 'raw':
      s.push({ title: t('usage.raw.download', 'Download'), code: `curl -O ${url}/path/to/file.txt` })
      if (hosted) s.push({ title: t('usage.raw.upload', 'Upload'), code: `curl -u USERNAME:USER_TOKEN --upload-file ./file.txt ${url}/path/to/file.txt` })
      break
    case 'nuget':
      s.push({ title: 'dotnet', code: `dotnet nuget add source ${url}/index.json -n ${n} -u USERNAME -p USER_TOKEN --store-password-in-clear-text` })
      if (hosted) s.push({ title: t('usage.nuget.push', 'Push'), code: `dotnet nuget push MyPkg.1.0.0.nupkg -s ${n} -k USER_TOKEN` })
      break
    case 'helm':
      s.push({ title: 'helm repo add', code: `helm repo add ${n} ${url}/ --username USERNAME --password USER_TOKEN\nhelm repo update` })
      if (hosted) s.push({ title: t('usage.helm.push', 'Upload a chart'), code: `curl -u USERNAME:USER_TOKEN --upload-file mychart-1.0.0.tgz ${url}/` })
      break
    case 'go':
      s.push({ title: 'GOPROXY', code: `export GOPROXY=${url}\nexport GONOSUMDB=\nexport GOSUMDB="sum.golang.org ${url}/sumdb"`, wide: true })
      break
    case 'apt':
      s.push({ title: t('usage.apt.sources', 'Sources'), file: '/etc/apt/sources.list.d/holiaokho.list', code: `curl -fsSL ${url}/repository-key.gpg | gpg --dearmor -o /etc/apt/keyrings/${n}.gpg\necho "deb [signed-by=/etc/apt/keyrings/${n}.gpg] ${url} ${rp.attributes.apt?.distribution ?? 'stable'} ${rp.attributes.apt?.component ?? 'main'}" > /etc/apt/sources.list.d/${n}.list\napt-get update`, wide: true })
      if (hosted) s.push({ title: t('usage.apt.upload', 'Upload a .deb'), code: `curl -u USERNAME:USER_TOKEN --upload-file mypkg_1.0_amd64.deb ${url}/` })
      break
    case 'yum':
      s.push({ title: '.repo', file: `/etc/yum.repos.d/${n}.repo`, code: `[${n}]\nname=${n}\nbaseurl=${url}/\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=1\ngpgkey=${url}/repository-key.gpg` })
      if (hosted) s.push({ title: t('usage.yum.upload', 'Upload an .rpm'), code: `curl -u USERNAME:USER_TOKEN --upload-file mypkg-1.0-1.x86_64.rpm ${url}/` })
      break
    case 'alpine': {
      const key = rp.attributes.alpine?.keyName ?? 'holiaokho.rsa.pub'
      s.push({ title: 'apk', file: '/etc/apk/repositories', code: `curl -fsSL ${url}/${key} -o /etc/apk/keys/${key}\necho "${url}/main" >> /etc/apk/repositories\napk update`, wide: true })
      break
    }
    case 'rubygems':
      s.push({ title: 'gem', code: `gem sources --add ${url}/\n# or in Gemfile:\nsource "${url}/"` })
      if (hosted) s.push({ title: 'gem push', code: `gem push --host ${url} mygem-1.0.0.gem` })
      break
    case 'cargo':
      s.push({ title: 'cargo', file: '.cargo/config.toml', code: `[registries]\n${n} = { index = "sparse+${url}/" }\n\n[net]\ngit-fetch-with-cli = true` })
      s.push({ title: 'cargo login', code: `cargo login --registry ${n} "Bearer USER_TOKEN"${hosted ? `\ncargo publish --registry ${n}` : ''}` })
      break
    case 'composer':
      s.push({ title: 'composer', code: `composer config repositories.${n} composer ${url}/\ncomposer config http-basic.${host} USERNAME USER_TOKEN` })
      break
    case 'conda':
      s.push({ title: 'conda', file: '~/.condarc', code: `channels:\n  - ${url}/main\n  - ${url}/conda-forge` })
      break
    case 'cran':
      s.push({ title: 'R', file: '~/.Rprofile', code: `options(repos = c(CRAN = "${url}/"))` })
      break
    case 'p2':
      s.push({ title: 'Eclipse update site', code: `${url}/` })
      break
    case 'cocoapods':
      s.push({ title: 'Podfile', file: 'Podfile', code: `source '${url}/'` })
      break
    case 'terraform':
      s.push({ title: t('usage.tf.mirror', 'Provider network mirror'), file: '~/.terraformrc', code: `provider_installation {\n  network_mirror {\n    url = "${url}/providers/"\n  }\n}` })
      s.push({ title: t('usage.tf.module', 'Module source'), code: `module "vpc" {\n  source  = "${host}/namespace/name/provider"\n  version = "1.0.0"\n}` })
      break
    case 'pub':
      s.push({ title: 'pub', code: `export PUB_HOSTED_URL=${url}\ndart pub get` })
      if (hosted) s.push({ title: 'dart pub publish', code: `dart pub token add ${url}\ndart pub publish` })
      break
    case 'gitlfs':
      s.push({ title: 'Git LFS', file: '.lfsconfig', code: `[lfs]\n\turl = ${url}/` })
      break
    case 'huggingface':
      s.push({ title: 'huggingface_hub', code: `export HF_ENDPOINT=${url}` })
      break
    case 'ansiblegalaxy':
      s.push({ title: 'ansible-galaxy', file: 'ansible.cfg', code: `[galaxy]\nserver_list = ${n}\n\n[galaxy_server.${n}]\nurl=${url}/\ntoken=USER_TOKEN` })
      break
    case 'conan':
      s.push({ title: 'conan', code: `conan remote add ${n} ${url}\nconan remote login ${n} USERNAME -p USER_TOKEN${hosted ? `\nconan upload "*" -r ${n} --confirm` : ''}` })
      break
    case 'swift':
      s.push({ title: 'swift package-registry', code: `swift package-registry set ${url}\nswift package-registry login ${url} --username USERNAME --password USER_TOKEN` })
      break
    default:
      s.push({ title: 'URL', code: url })
  }
  return s.map((x) => ({ ...x, code: x.code.replace(/\n{3,}/g, '\n\n') })).concat([{ title: '', code: cred, wide: true }]).filter((x) => x.title !== '')
}

export function UsageSnippets({ repo }: { repo: Repository }) {
  const { t } = useTranslation()
  const snips = snippetsFor(repo, t as any)
  return (
    <div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(380px, 1fr))', gap: 16 }}>
        {snips.map((s, i) => (
          <div key={i} style={s.wide ? { gridColumn: '1 / -1' } : undefined}>
            <CodeBlock code={s.code} title={s.title} filename={s.file ?? s.desc} />
          </div>
        ))}
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 16, fontSize: 13 }}>
        <span style={{ width: 8, height: 8, background: 'var(--hlk-amber)', borderRadius: 2 }} />
        {t('usage.cred', 'Credentials: use a User Token (username + token as the password).')}
        <Link to="/me/tokens">{t('usage.createToken', 'Create my token →')}</Link>
      </div>
    </div>
  )
}
