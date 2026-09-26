import { useEffect, useMemo, useState } from 'react'
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, HolderOutlined } from '@ant-design/icons'
import { App, Button, Checkbox, Form, Input, InputNumber, Modal, Radio, Select, Switch } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post } from '@/api/client'
import type { CleanupPolicy, RepoAttributes, RepoType, Repository, RoutingRule, Storage, VulnSummary } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { Section, SecretHint, useErrorText } from './Common'
import { CodeBlock } from './Copyable'

export const defaultRemote: Record<string, string> = {
  maven: 'https://repo1.maven.org/maven2/', npm: 'https://registry.npmjs.org/', docker: 'https://registry-1.docker.io', pypi: 'https://pypi.org/',
  nuget: 'https://api.nuget.org/v3/index.json', go: 'https://proxy.golang.org', rubygems: 'https://rubygems.org/', cargo: 'https://index.crates.io/',
  composer: 'https://packagist.org/', conda: 'https://conda.anaconda.org/conda-forge/', cran: 'https://cloud.r-project.org/', apt: 'http://deb.debian.org/debian',
  yum: 'https://dl.fedoraproject.org/pub/epel/9/Everything/x86_64/', alpine: 'https://dl-cdn.alpinelinux.org/alpine/v3.20/', helm: 'https://charts.bitnami.com/bitnami',
  pub: 'https://pub.dev', terraform: 'https://registry.terraform.io', huggingface: 'https://huggingface.co', ansiblegalaxy: 'https://galaxy.ansible.com',
  conan: 'https://center2.conan.io', cocoapods: 'https://cdn.cocoapods.org/', p2: 'https://download.eclipse.org/releases/latest/', swift: '', raw: '', gitlfs: '',
}

export interface RepoFormValues {
  name: string; storage: string; online: boolean; routingRuleId?: string | null; cleanupPolicies: string[]
  attributes: RepoAttributes
}

export function fromRepo(r: Repository, storages: Storage[]): RepoFormValues {
  return {
    name: r.name, storage: storages.find((s) => s.id === r.storageId)?.name ?? 'default', online: r.online, routingRuleId: r.routingRuleId ?? undefined,
    cleanupPolicies: r.cleanupPolicies ?? [], attributes: JSON.parse(JSON.stringify(r.attributes ?? {})),
  }
}

export function initialValues(format: string, type: RepoType): RepoFormValues {
  const attributes: RepoAttributes = {}
  if (type === 'proxy') attributes.proxy = { remoteUrl: defaultRemote[format] ?? '', contentMaxAge: 1440, metadataMaxAge: 1440, negativeCacheTtl: 1440, autoBlock: true, blocked: false }
  if (type === 'hosted') attributes.hosted = { writePolicy: 'allow_once' }
  if (type === 'group') attributes.group = { members: [] }
  if (format === 'maven') attributes.maven = { layoutPolicy: 'STRICT', versionPolicy: type === 'hosted' ? 'RELEASE' : 'MIXED' }
  if (format === 'docker') attributes.docker = { httpPort: 0, httpsPort: 0, pathEnabled: true, forceBasicAuth: false, indexType: 'HUB' }
  if (format === 'apt') attributes.apt = { distribution: 'stable', component: 'main' }
  if (format === 'yum') attributes.yum = { repodataDepth: 0 }
  if (format === 'alpine') attributes.alpine = { keyName: 'holiaokho.rsa.pub' }
  return { name: '', storage: 'default', online: true, cleanupPolicies: [], attributes }
}

function KeyGen({ kind, onKey }: { kind: 'pgp' | 'rsa'; onKey: (priv: string) => void }) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [pub, setPub] = useState<string | null>(null)
  const [form] = Form.useForm()
  const gen = async () => {
    setBusy(true)
    try {
      const v = form.getFieldsValue()
      const r = await post<{ privateKey: string; publicKey: string }>(kind === 'pgp' ? 'system/pgp-key' : 'system/rsa-key', v)
      onKey(r.privateKey)
      setPub(r.publicKey)
    } catch (e) {
      message.error(errText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Button size="small" onClick={() => { setPub(null); setOpen(true) }}>{t('repos.genKey', 'Generate key')}</Button>
      <Modal open={open} onCancel={() => setOpen(false)} footer={null} title={t('repos.genKeyTitle', 'Generate a signing key')} width={620}>
        {!pub ? (
          <Form form={form} layout="vertical" initialValues={{ name: 'Holiaokho', email: 'repo@holiaokho.local' }}>
            {kind === 'pgp' && (
              <>
                <Form.Item name="name" label={t('common.name', 'Name')}><Input /></Form.Item>
                <Form.Item name="email" label={t('repos.keyEmail', 'Email')}><Input /></Form.Item>
              </>
            )}
            <p style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{t('repos.genKeyHint', 'The private key is stored encrypted with the repository. Clients need the public key, served from the repository URL.')}</p>
            <Button type="primary" loading={busy} onClick={gen}>{t('repos.genKey', 'Generate key')}</Button>
          </Form>
        ) : (
          <>
            <p style={{ fontSize: 13 }}>{t('repos.genKeyDone', 'Private key filled in. Public key (also available at <repo url>/repository-key.gpg after saving):')}</p>
            <CodeBlock code={pub} />
            <div style={{ marginTop: 12, textAlign: 'right' }}><Button type="primary" onClick={() => setOpen(false)}>{t('common.close', 'Close')}</Button></div>
          </>
        )}
      </Modal>
    </>
  )
}

export function RepoForm({ format, type, value, onChange, editing, existing }: { format: string; type: RepoType; value: RepoFormValues; onChange: (v: RepoFormValues) => void; editing?: boolean; existing?: string }) {
  const { t } = useTranslation()
  const storages = useQuery({ queryKey: ['storages'], queryFn: () => get<Storage[]>('storages') })
  const rules = useQuery({ queryKey: ['routing-rules'], queryFn: () => get<RoutingRule[]>('routing-rules') })
  const policies = useQuery({ queryKey: ['cleanup-policies'], queryFn: () => get<CleanupPolicy[]>('cleanup-policies') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories'), enabled: type === 'group' })
  const { can } = useAuth()
  // The server says which formats OSV can check; the switch is offered only
  // where it would do something.
  const vulns = useQuery({ queryKey: ['vuln-summary'], queryFn: () => get<VulnSummary>('vulnerabilities/summary'), enabled: type !== 'group' && can('app:search', 'read') })
  const scannable = !!vulns.data?.enabled && vulns.data.coveredFormats.includes(format)
  const a = value.attributes
  const set = (patch: Partial<RepoFormValues>) => onChange({ ...value, ...patch })
  const setAttr = <K extends keyof RepoAttributes>(k: K, patch: Partial<NonNullable<RepoAttributes[K]>>) => onChange({ ...value, attributes: { ...a, [k]: { ...(a[k] ?? {}), ...patch } } })
  const nameOk = /^[A-Za-z0-9._-]+$/.test(value.name)
  const members = useMemo(() => (repos.data ?? []).filter((r) => r.format === format && r.type !== 'group' && r.name !== existing), [repos.data, format, existing])
  const [forever, setForever] = useState((a.proxy?.contentMaxAge ?? 0) < 0)
  useEffect(() => setForever((a.proxy?.contentMaxAge ?? 0) < 0), [a.proxy?.contentMaxAge])
  const label = (k: string, d: string) => <span style={{ fontSize: 12, fontWeight: 500 }}>{t(k, d)}</span>
  const hint = (x: string) => <div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)', marginTop: 4 }}>{x}</div>
  const secretField = (k: 'apt' | 'yum' | 'alpine', kind: 'pgp' | 'rsa') => (
    <Form.Item label={<span style={{ display: 'flex', gap: 10, alignItems: 'center' }}>{label('repos.f.signingKey', 'Signing key (private, PEM/armored)')}<KeyGen kind={kind} onKey={(priv) => setAttr(k, { signingKey: priv } as any)} /></span>}>
      <Input.TextArea rows={4} className="hlk-mono" value={(a[k] as any)?.signingKey ?? ''} onChange={(e) => setAttr(k, { signingKey: e.target.value } as any)} placeholder={editing ? '***' : undefined} style={{ fontSize: 11 }} />
      <SecretHint />
    </Form.Item>
  )

  return (
    <Form layout="vertical" component="div" size="middle">
      <Section title={t('repos.sec.basic', 'Basic')} style={{ marginBottom: 16 }}>
        <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <Form.Item label={label('common.name', 'Name')} validateStatus={value.name && !nameOk ? 'error' : undefined} help={value.name && !nameOk ? t('repos.nameInvalid', 'Only letters, digits, . _ -') : undefined}>
            <Input className="hlk-mono" value={value.name} onChange={(e) => set({ name: e.target.value })} disabled={editing} placeholder={`${format}-${type}`} />
            {!editing && hint('A–Z a–z 0–9 . _ -')}
          </Form.Item>
          <Form.Item label={label('nav.storages', 'Storage')}>
            <Select value={value.storage} onChange={(v) => set({ storage: v })} disabled={editing} options={(storages.data ?? []).map((s) => ({ value: s.name, label: `${s.name} (${s.type})` }))} />
          </Form.Item>
        </div>
        <Form.Item label={label('repos.online', 'Online')} style={{ marginBottom: 0 }}>
          <Switch checked={value.online} onChange={(v) => set({ online: v })} /> <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginLeft: 8 }}>{t('repos.onlineHint', 'Offline repositories reject all client requests.')}</span>
        </Form.Item>
        {scannable && (
          <Form.Item label={label('repos.f.vulnScan', 'Vulnerability scanning')} style={{ marginTop: 12, marginBottom: 0 }}>
            <Switch checked={a.vulnerabilities?.scan !== false} onChange={(v) => setAttr('vulnerabilities', { scan: v })} /> <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginLeft: 8 }}>{t('repos.f.vulnScanHint', 'Check the packages in this repository against OSV for known vulnerabilities.')}</span>
          </Form.Item>
        )}
      </Section>

      {type === 'proxy' && (
        <Section title={t('repos.sec.proxy', 'Proxy · upstream')} style={{ marginBottom: 16 }}>
          <Form.Item label={label('repos.f.remoteUrl', 'Remote URL')}>
            <Input className="hlk-mono" value={a.proxy?.remoteUrl ?? ''} onChange={(e) => setAttr('proxy', { remoteUrl: e.target.value })} placeholder="https://" />
          </Form.Item>
          <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr 1fr', gap: 16 }}>
            <Form.Item label={label('repos.f.contentMaxAge', 'Content max age')}>
              <InputNumber style={{ width: '100%' }} addonAfter={t('common.min', 'min')} value={forever ? undefined : a.proxy?.contentMaxAge} disabled={forever} onChange={(v) => setAttr('proxy', { contentMaxAge: v ?? 0 })} min={0} />
              <Checkbox checked={forever} onChange={(e) => { setForever(e.target.checked); setAttr('proxy', { contentMaxAge: e.target.checked ? -1 : 1440 }) }} style={{ marginTop: 6, fontSize: 12 }}>{t('repos.f.cacheForever', 'Cache forever')}</Checkbox>
            </Form.Item>
            <Form.Item label={label('repos.f.metadataMaxAge', 'Metadata max age')}>
              <InputNumber style={{ width: '100%' }} addonAfter={t('common.min', 'min')} value={a.proxy?.metadataMaxAge} onChange={(v) => setAttr('proxy', { metadataMaxAge: v ?? 0 })} min={0} />
              {hint(t('repos.f.metadataHint', 'Index / version lists re-check the upstream after this.'))}
            </Form.Item>
            <Form.Item label={label('repos.f.negativeTtl', 'Negative cache TTL')}>
              <InputNumber style={{ width: '100%' }} addonAfter={t('common.min', 'min')} value={a.proxy?.negativeCacheTtl} onChange={(v) => setAttr('proxy', { negativeCacheTtl: v ?? 0 })} min={0} />
              {hint(t('repos.f.negativeHint', 'Remember upstream 404s for this long. 0 disables.'))}
            </Form.Item>
          </div>
          <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item label={label('repos.f.upstreamUser', 'Upstream username')}><Input value={a.proxy?.username ?? ''} onChange={(e) => setAttr('proxy', { username: e.target.value })} autoComplete="off" /></Form.Item>
            <Form.Item label={label('repos.f.upstreamPass', 'Upstream password')}>
              <Input.Password value={a.proxy?.password ?? ''} onChange={(e) => setAttr('proxy', { password: e.target.value })} autoComplete="new-password" placeholder={editing ? '***' : undefined} />
              {editing && <SecretHint />}
            </Form.Item>
          </div>
          <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item label={label('repos.f.retries', 'Upstream retries')}>
              <InputNumber style={{ width: '100%' }} value={a.proxy?.retries ?? null} onChange={(v) => setAttr('proxy', { retries: v ?? null })} min={0} max={10} placeholder={t('repos.f.retriesDefault', 'Server default')} />
              {hint(t('repos.f.retriesHint', 'A failed download is tried again this many times (dropped connection, timeout, 429/502/503/504). Empty uses the server\'s proxy.retries, 2 unless configured.'))}
            </Form.Item>
          </div>
          <div style={{ display: 'flex', gap: 24 }}>
            <Checkbox checked={!!a.proxy?.autoBlock} onChange={(e) => setAttr('proxy', { autoBlock: e.target.checked })}>{t('repos.f.autoBlock', 'Auto-block when the upstream is unreachable')}</Checkbox>
            <Checkbox checked={!!a.proxy?.blocked} onChange={(e) => setAttr('proxy', { blocked: e.target.checked })}>{t('repos.f.blocked', 'Blocked (serve cache only)')}</Checkbox>
          </div>
        </Section>
      )}

      {type === 'hosted' && (
        <Section title={t('repos.sec.hosted', 'Hosted · write policy')} style={{ marginBottom: 16 }}>
          <Radio.Group value={a.hosted?.writePolicy ?? 'allow_once'} onChange={(e) => setAttr('hosted', { writePolicy: e.target.value })} style={{ display: 'grid', gap: 8 }}>
            <Radio value="allow_once"><b>allow_once</b> — {t('repos.wp.allowOnce', 'a path can be written once; redeploying the same version is rejected (Maven SNAPSHOT excepted). Recommended.')}</Radio>
            <Radio value="allow"><b>allow</b> — {t('repos.wp.allow', 'existing paths can be overwritten.')}</Radio>
            <Radio value="deny"><b>deny</b> — {t('repos.wp.deny', 'read-only; no uploads.')}</Radio>
          </Radio.Group>
        </Section>
      )}

      {type === 'group' && (
        <Section title={t('repos.sec.group', 'Group · members (order = resolution order)')} style={{ marginBottom: 16 }}>
          <GroupMembers value={a.group?.members ?? []} candidates={members} onChange={(ms) => setAttr('group', { members: ms })} />
        </Section>
      )}

      {format === 'maven' && (
        <Section title="Maven" style={{ marginBottom: 16 }}>
          <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item label={label('repos.f.layoutPolicy', 'Layout policy')}>
              <Select value={a.maven?.layoutPolicy ?? 'STRICT'} onChange={(v) => setAttr('maven', { layoutPolicy: v })} options={[{ value: 'STRICT' }, { value: 'PERMISSIVE' }]} />
            </Form.Item>
            <Form.Item label={label('repos.f.versionPolicy', 'Version policy')}>
              <Select value={a.maven?.versionPolicy ?? 'MIXED'} onChange={(v) => setAttr('maven', { versionPolicy: v })} options={[{ value: 'RELEASE' }, { value: 'SNAPSHOT' }, { value: 'MIXED' }]} />
            </Form.Item>
          </div>
        </Section>
      )}

      {format === 'docker' && (
        <Section title={t('repos.sec.docker', 'Docker · access (any combination)')} style={{ marginBottom: 16 }}>
          <div style={{ display: 'grid', gap: 10 }}>
            <div className={`hlk-optioncard${a.docker?.pathEnabled !== false ? ' selected' : ''}`} onClick={() => setAttr('docker', { pathEnabled: !(a.docker?.pathEnabled !== false) })}>
              <div style={{ display: 'grid', gridTemplateColumns: '20px 150px 1fr', gap: 12, alignItems: 'center' }}>
                <Checkbox checked={a.docker?.pathEnabled !== false} />
                <b>{t('repos.d.path', 'Path mode')}</b>
                <span className="hlk-mono" style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{window.location.host}/{value.name || 'repo'}/&lt;image&gt;</span>
              </div>
            </div>
            <div className={`hlk-optioncard${a.docker?.httpPort ? ' selected' : ''}`}>
              <div style={{ display: 'grid', gridTemplateColumns: '20px 150px 1fr', gap: 12, alignItems: 'center' }}>
                <Checkbox checked={!!a.docker?.httpPort} onChange={(e) => setAttr('docker', { httpPort: e.target.checked ? 5000 : 0 })} />
                <b>{t('repos.d.port', 'HTTP port')}</b>
                <span style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                  <InputNumber size="small" min={0} max={65535} value={a.docker?.httpPort ?? 0} onChange={(v) => setAttr('docker', { httpPort: v ?? 0 })} style={{ width: 110 }} />
                  <span className="hlk-mono" style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{window.location.hostname}:{a.docker?.httpPort || 'PORT'}/&lt;image&gt;</span>
                </span>
              </div>
            </div>
            <div className={`hlk-optioncard${a.docker?.httpsPort ? ' selected' : ''}`}>
              <div style={{ display: 'grid', gridTemplateColumns: '20px 150px 1fr', gap: 12, alignItems: 'center' }}>
                <Checkbox checked={!!a.docker?.httpsPort} onChange={(e) => setAttr('docker', { httpsPort: e.target.checked ? 5443 : 0 })} />
                <b>{t('repos.d.tls', 'HTTPS port')}</b>
                <span style={{ display: 'grid', gridTemplateColumns: '110px 1fr 1fr', gap: 8 }}>
                  <InputNumber size="small" min={0} max={65535} value={a.docker?.httpsPort ?? 0} onChange={(v) => setAttr('docker', { httpsPort: v ?? 0 })} />
                  <Input size="small" className="hlk-mono" placeholder="/path/to/cert.pem" value={a.docker?.tlsCert ?? ''} onChange={(e) => setAttr('docker', { tlsCert: e.target.value })} />
                  <Input size="small" className="hlk-mono" placeholder="/path/to/key.pem" value={a.docker?.tlsKey ?? ''} onChange={(e) => setAttr('docker', { tlsKey: e.target.value })} />
                </span>
              </div>
            </div>
            <div className={`hlk-optioncard${a.docker?.subdomain ? ' selected' : ''}`}>
              <div style={{ display: 'grid', gridTemplateColumns: '20px 150px 1fr', gap: 12, alignItems: 'center' }}>
                <Checkbox checked={!!a.docker?.subdomain} onChange={(e) => setAttr('docker', { subdomain: e.target.checked ? value.name : '' })} />
                <b>{t('repos.d.subdomain', 'Subdomain')}</b>
                <span style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                  <Input size="small" className="hlk-mono" value={a.docker?.subdomain ?? ''} onChange={(e) => setAttr('docker', { subdomain: e.target.value })} style={{ width: 160 }} />
                  <span className="hlk-mono" style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{a.docker?.subdomain || 'name'}.{window.location.host}/&lt;image&gt;</span>
                </span>
              </div>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 24, marginTop: 14, alignItems: 'center' }}>
            <Checkbox checked={!!a.docker?.forceBasicAuth} onChange={(e) => setAttr('docker', { forceBasicAuth: e.target.checked })}>{t('repos.d.forceBasic', 'Force basic auth (no anonymous pulls)')}</Checkbox>
            {type === 'proxy' && (
              <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                {t('repos.d.indexType', 'Index type')}
                <Select size="small" value={a.docker?.indexType ?? 'HUB'} onChange={(v) => setAttr('docker', { indexType: v })} options={[{ value: 'HUB', label: 'HUB (Docker Hub)' }, { value: 'REGISTRY' }, { value: 'CUSTOM' }]} style={{ width: 180 }} />
              </span>
            )}
          </div>
        </Section>
      )}

      {format === 'apt' && (
        <Section title="APT" style={{ marginBottom: 16 }}>
          {type === 'hosted' ? (
            <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <Form.Item label={label('repos.f.distribution', 'Distribution')}><Input className="hlk-mono" value={a.apt?.distribution ?? ''} onChange={(e) => setAttr('apt', { distribution: e.target.value })} /></Form.Item>
              <Form.Item label={label('repos.f.component', 'Component')}><Input className="hlk-mono" value={a.apt?.component ?? ''} onChange={(e) => setAttr('apt', { component: e.target.value })} /></Form.Item>
            </div>
          ) : (
            <Checkbox checked={!!a.apt?.flat} onChange={(e) => setAttr('apt', { flat: e.target.checked })}>{t('repos.f.flat', 'Upstream is a flat repository (no dists/)')}</Checkbox>
          )}
          {type === 'hosted' && (
            <>
              {secretField('apt', 'pgp')}
              <Form.Item label={label('repos.f.passphrase', 'Key passphrase')}><Input.Password value={a.apt?.passphrase ?? ''} onChange={(e) => setAttr('apt', { passphrase: e.target.value })} autoComplete="new-password" /></Form.Item>
            </>
          )}
        </Section>
      )}
      {format === 'yum' && (
        <Section title="YUM" style={{ marginBottom: 16 }}>
          <Form.Item label={label('repos.f.repodataDepth', 'Repodata depth')}><InputNumber min={0} max={5} value={a.yum?.repodataDepth ?? 0} onChange={(v) => setAttr('yum', { repodataDepth: v ?? 0 })} />{hint(t('repos.f.repodataHint', 'Directory depth at which repodata/ is generated (0 = root).'))}</Form.Item>
          {type === 'hosted' && (
            <>
              {secretField('yum', 'pgp')}
              <Form.Item label={label('repos.f.passphrase', 'Key passphrase')}><Input.Password value={a.yum?.passphrase ?? ''} onChange={(e) => setAttr('yum', { passphrase: e.target.value })} autoComplete="new-password" /></Form.Item>
            </>
          )}
        </Section>
      )}
      {format === 'alpine' && type === 'hosted' && (
        <Section title="Alpine" style={{ marginBottom: 16 }}>
          {secretField('alpine', 'rsa')}
          <Form.Item label={label('repos.f.keyName', 'Public key file name')}><Input className="hlk-mono" value={a.alpine?.keyName ?? ''} onChange={(e) => setAttr('alpine', { keyName: e.target.value })} />{hint(t('repos.f.keyNameHint', 'Installed by clients under /etc/apk/keys/'))}</Form.Item>
        </Section>
      )}
      {format === 'cargo' && type === 'hosted' && (
        <Section title="Cargo" style={{ marginBottom: 16 }}>
          <Form.Item label={label('repos.f.downloadUrl', 'Download URL template')}><Input className="hlk-mono" value={a.cargo?.downloadUrl ?? ''} onChange={(e) => setAttr('cargo', { downloadUrl: e.target.value })} placeholder={t('repos.f.downloadUrlHint', 'leave empty for the default')} /></Form.Item>
        </Section>
      )}

      <Section title={t('repos.sec.optional', 'Optional')} style={{ marginBottom: 16 }}>
        <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <Form.Item label={label('nav.routing', 'Routing rule')}>
            <Select allowClear value={value.routingRuleId ?? undefined} onChange={(v) => set({ routingRuleId: v ?? null })} options={(rules.data ?? []).map((r) => ({ value: r.id, label: `${r.name} (${r.mode})` }))} placeholder={t('common.none', 'None')} />
          </Form.Item>
          <Form.Item label={label('nav.cleanup', 'Cleanup policies')}>
            <Select mode="multiple" value={value.cleanupPolicies} onChange={(v) => set({ cleanupPolicies: v })} options={(policies.data ?? []).filter((p) => !p.format || p.format === format).map((p) => ({ value: p.id, label: p.name }))} placeholder={t('common.none', 'None')} />
          </Form.Item>
        </div>
      </Section>
    </Form>
  )
}

// GroupMembers is the group's member list in resolution order: the first
// member that has a path answers, so order matters (a proxy placed above
// maven-central is asked first for everything). New members are added at the
// end, and rows move by dragging or with the arrows.
function GroupMembers({ value, candidates, onChange }: { value: string[]; candidates: Repository[]; onChange: (ms: string[]) => void }) {
  const { t } = useTranslation()
  const [dragFrom, setDragFrom] = useState<number | null>(null)
  const [dragOver, setDragOver] = useState<number | null>(null)
  const typeOf = (n: string) => candidates.find((c) => c.name === n)?.type
  const move = (from: number, to: number) => {
    if (to < 0 || to >= value.length || from === to) return
    const ms = [...value]
    const [m] = ms.splice(from, 1)
    ms.splice(to, 0, m)
    onChange(ms)
  }
  const available = candidates.filter((c) => !value.includes(c.name))
  const deployTo = value.find((m) => typeOf(m) === 'hosted')
  return (
    <div style={{ maxWidth: 560 }}>
      <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginBottom: 10 }}>
        {t('repos.g.orderHint', 'Members are asked from top to bottom; the first one that has the file answers. Drag or use the arrows to reorder.')}
      </div>
      {value.length === 0 && <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginBottom: 8 }}>{t('repos.g.none', 'No members yet.')}</div>}
      {value.map((m, i) => (
        <div
          key={m}
          className={`hlk-realm${dragOver === i ? ' over' : ''}`}
          draggable
          onDragStart={() => setDragFrom(i)}
          onDragOver={(e) => { e.preventDefault(); setDragOver(i) }}
          onDragLeave={() => setDragOver((v) => (v === i ? null : v))}
          onDrop={(e) => { e.preventDefault(); if (dragFrom !== null) move(dragFrom, i); setDragFrom(null); setDragOver(null) }}
          onDragEnd={() => { setDragFrom(null); setDragOver(null) }}
        >
          <HolderOutlined className="hlk-realm-grip" />
          <span className="hlk-realm-no">{i + 1}</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <span className="hlk-mono" style={{ fontSize: 13, fontWeight: 500 }}>{m}</span>
            <span style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)', marginLeft: 8 }}>{typeOf(m) ? t(`type.${typeOf(m)}`, typeOf(m)!) : t('repos.g.missing', 'not found')}</span>
          </div>
          <Button type="text" size="small" aria-label={t('repos.g.up', 'Move up')} disabled={i === 0} icon={<ArrowUpOutlined />} onClick={() => move(i, i - 1)} />
          <Button type="text" size="small" aria-label={t('repos.g.down', 'Move down')} disabled={i === value.length - 1} icon={<ArrowDownOutlined />} onClick={() => move(i, i + 1)} />
          <Button type="text" size="small" danger aria-label={t('repos.g.remove', 'Remove')} icon={<DeleteOutlined />} onClick={() => onChange(value.filter((x) => x !== m))} />
        </div>
      ))}
      <Select
        style={{ width: '100%', marginTop: 6 }} showSearch value={null as unknown as string} disabled={available.length === 0}
        placeholder={available.length ? t('repos.g.add', 'Add a member (added last)') : t('repos.g.allAdded', 'Every {{format}} repository is already a member', { format: candidates[0]?.format ?? '' })}
        options={available.map((c) => ({ value: c.name, label: `${c.name} (${t(`type.${c.type}`, c.type)})` }))}
        onChange={(v) => v && onChange([...value, v])}
      />
      <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginTop: 8 }}>
        {deployTo
          ? t('repos.g.deployTo', 'Deployments sent to this group are stored in {{name}}, its first hosted member.', { name: deployTo })
          : t('repos.g.noDeploy', 'No hosted member, so this group does not accept deployments.')}
      </div>
    </div>
  )
}
