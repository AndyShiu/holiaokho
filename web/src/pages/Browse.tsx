import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Alert, App, Button, Checkbox, Drawer, Input, Progress, Segmented, Skeleton, Table, Tabs, Tag, Upload } from 'antd'
import { CloudUploadOutlined, FileOutlined, FolderOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError, api, get } from '@/api/client'
import type { Asset, BrowseResult, Package, Repository } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { FormatIcon } from '@/components/FormatIcon'
import { TypeTag } from '@/components/TypeTag'
import { Copyable } from '@/components/Copyable'
import { EmptyState, useErrorText } from '@/components/Common'
import { fmtBytes, RelTime } from '@/components/Format'
import { AssetDrawer, PackageDrawer } from '@/components/Drawers'
import { UsageSnippets, repoBase } from '@/components/UsageSnippets'
import { formatInfo } from '@/theme/tokens'

function dirName(path: string) {
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? path
}

export function RepoContent({ repo, path, onNavigate, showPackages = true, initialTab = 'content' }: { repo: Repository; path: string; onNavigate: (p: string) => void; showPackages?: boolean; initialTab?: string }) {
  const { t } = useTranslation()
  const { canRepo } = useAuth()
  const qc = useQueryClient()
  const [tab, setTab] = useState(initialTab)
  const [asset, setAsset] = useState<string | null>(null)
  const [pkg, setPkg] = useState<string | null>(null)
  const [pkgQ, setPkgQ] = useState('')
  const [page, setPage] = useState(0)
  const [upload, setUpload] = useState(false)
  const browse = useQuery({ queryKey: ['browse', repo.name, path], queryFn: () => get<BrowseResult>(`repositories/${repo.name}/browse`, { path }), staleTime: 30000 })
  const packages = useQuery({ queryKey: ['packages', repo.name, pkgQ, page], queryFn: () => get<Package[]>(`repositories/${repo.name}/packages`, { q: pkgQ, limit: 50, offset: page * 50 }), enabled: tab === 'packages' })
  const crumbs = path.split('/').filter(Boolean)
  const isDocker = repo.format === 'docker'
  const canWrite = canRepo(repo.name, repo.format, 'write')

  const rows = useMemo(() => {
    const d = browse.data
    if (!d) return []
    const dirs = (d.directories ?? []).map((x) => ({ key: 'd:' + x, dir: true, name: dirName(x), path: x, file: undefined as Asset | undefined }))
    const files = (d.files ?? []).map((f) => ({ key: 'f:' + f.id, dir: false, name: dirName(f.path), path: f.path, file: f }))
    return [...dirs, ...files]
  }, [browse.data])

  return (
    <>
      <Tabs
        activeKey={tab}
        onChange={setTab}
        tabBarExtraContent={
          <div style={{ display: 'flex', gap: 8 }}>
            {canWrite && repo.type === 'hosted' && <Button size="small" icon={<CloudUploadOutlined />} onClick={() => setUpload(true)}>{t('browse.upload', 'Upload')}</Button>}
            <Button size="small" icon={<ReloadOutlined />} onClick={() => { qc.invalidateQueries({ queryKey: ['browse', repo.name] }); qc.invalidateQueries({ queryKey: ['packages', repo.name] }) }} />
          </div>
        }
        items={[
          {
            key: 'content', label: t('browse.content', 'Content'),
            children: (
              <>
                {repo.type === 'proxy' && <Alert type="info" showIcon message={t('browse.proxyHint', 'Only cached content is listed here — this is not the full upstream catalogue.')} style={{ marginBottom: 12 }} />}
                {repo.type === 'group' && (
                  <Alert type="info" showIcon style={{ marginBottom: 12 }} message={<span>{t('browse.groupHint', 'Merged from {{n}} members:', { n: repo.attributes.group?.members?.length ?? 0 })} {(repo.attributes.group?.members ?? []).map((m, i) => <span key={m}>{i > 0 && ', '}<Link to={`/browse/${m}`}>{m}</Link></span>)}</span>} />
                )}
                {!repo.online && <Alert type="warning" showIcon message={t('browse.offline', 'This repository is offline: client requests are rejected.')} style={{ marginBottom: 12 }} />}
                <div className="hlk-breadcrumb hlk-mono" style={{ fontSize: 13, marginBottom: 10, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  <a onClick={() => onNavigate('')}>{repo.name}</a>
                  {crumbs.map((c, i) => (
                    <span key={i}>
                      <span style={{ color: 'var(--hlk-text-tertiary)' }}> / </span>
                      {i === crumbs.length - 1 ? <span>{c}</span> : <a onClick={() => onNavigate(crumbs.slice(0, i + 1).join('/'))}>{c}</a>}
                    </span>
                  ))}
                </div>
                {browse.isLoading ? (
                  <Skeleton active paragraph={{ rows: 6 }} />
                ) : browse.isError ? (
                  <Alert type="error" showIcon message={useErrorTextStatic(browse.error)} />
                ) : (rows.length === 0 && !path) ? (
                  <EmptyState title={t('browse.empty', 'This repository has no content yet')} hint={repo.type === 'proxy' ? t('browse.emptyProxy', 'Content appears here after the first download through the proxy.') : repo.type === 'hosted' ? t('browse.emptyHosted', 'Upload something or push from a client.') : undefined} action={canWrite && repo.type === 'hosted' ? <Button type="primary" onClick={() => setUpload(true)}>{t('browse.upload', 'Upload')}</Button> : undefined} />
                ) : (
                  <Table
                    size="small" rowKey="key" pagination={rows.length > 200 ? { pageSize: 200, showSizeChanger: false } : false} dataSource={rows} className="hlk-table hlk-clickable"
                    onRow={(r) => ({ onClick: () => (r.dir ? onNavigate(r.path) : setAsset(r.file!.id)) })}
                    columns={[
                      {
                        title: t('common.name', 'Name'), dataIndex: 'name',
                        render: (x: string, r) => (
                          <span className="hlk-mono" style={{ fontSize: 12.5, display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                            {r.dir ? <FolderOutlined style={{ color: 'var(--hlk-amber)' }} /> : <FileOutlined style={{ color: 'var(--hlk-text-tertiary)' }} />}
                            {isDocker && crumbs[crumbs.length - 1] === 'manifests' && !r.dir ? <span>{x} <Tag style={{ marginLeft: 4 }}>tag</Tag></span> : isDocker && crumbs[crumbs.length - 1] === 'blobs' && !r.dir ? x.replace('sha256:', '').slice(0, 12) + '…' : x}
                          </span>
                        ),
                      },
                      { title: t('asset.size', 'Size'), width: 100, render: (_: unknown, r) => (r.dir ? '' : fmtBytes(r.file!.size)) },
                      { title: t('common.updated', 'Updated'), width: 130, render: (_: unknown, r) => (r.dir ? '' : <RelTime value={r.file!.updatedAt} />) },
                      ...(repo.type !== 'hosted' ? [{ title: t('asset.cacheExpires', 'Cache expires'), width: 130, render: (_: unknown, r: any) => (r.dir ? '' : r.file!.cacheExpiresAt ? <span style={{ color: new Date(r.file!.cacheExpiresAt) < new Date() ? 'var(--hlk-text-tertiary)' : undefined }}><RelTime value={r.file!.cacheExpiresAt} /></span> : '—') }] : []),
                      { title: '', width: 120, render: (_: unknown, r) => (r.dir ? null : <span onClick={(e) => e.stopPropagation()}><a href={`${repoBase(repo)}/${r.path}`} target="_blank">{t('common.download', 'Download')}</a> · <a onClick={() => setAsset(r.file!.id)}>{t('common.details', 'Details')}</a></span>) },
                    ]}
                  />
                )}
              </>
            ),
          },
          ...(showPackages ? [{
            key: 'packages', label: <span>{t('browse.packages', 'Packages')}{repo.stats ? <span className="hlk-mono" style={{ marginLeft: 6, fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{repo.stats.packages.toLocaleString()}</span> : null}</span>,
            children: (
              <>
                <Input prefix={<SearchOutlined />} placeholder={t('browse.filterPackages', 'Filter by name or namespace')} value={pkgQ} onChange={(e) => { setPkgQ(e.target.value); setPage(0) }} allowClear style={{ width: 320, marginBottom: 12 }} />
                <Table<Package>
                  size="small" rowKey="id" loading={packages.isLoading} dataSource={packages.data ?? []} className="hlk-table hlk-clickable"
                  onRow={(p) => ({ onClick: () => setPkg(p.id) })}
                  pagination={{ current: page + 1, pageSize: 50, total: (packages.data?.length ?? 0) < 50 ? page * 50 + (packages.data?.length ?? 0) : (page + 2) * 50, onChange: (p) => setPage(p - 1), showSizeChanger: false }}
                  columns={[
                    { title: t('package.namespace', 'Namespace'), dataIndex: 'namespace', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x || '—'}</span> },
                    { title: t('common.name', 'Name'), dataIndex: 'name', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500 }}>{x}</span> },
                    { title: t('package.version', 'Version'), dataIndex: 'version', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
                    { title: t('asset.lastDownloaded', 'Last downloaded'), dataIndex: 'lastDownloadedAt', width: 140, render: (x: string) => <RelTime value={x} empty={t('common.never', 'never')} /> },
                    { title: t('common.created', 'Created'), dataIndex: 'createdAt', width: 130, render: (x: string) => <RelTime value={x} /> },
                  ]}
                />
              </>
            ),
          }] : []),
        ]}
      />
      <AssetDrawer assetId={asset} repo={repo} onClose={() => setAsset(null)} onDeleted={() => { qc.invalidateQueries({ queryKey: ['browse', repo.name] }); qc.invalidateQueries({ queryKey: ['packages', repo.name] }) }} />
      <PackageDrawer packageId={pkg} onClose={() => setPkg(null)} onAsset={(id) => setAsset(id)} onDeleted={() => qc.invalidateQueries({ queryKey: ['packages', repo.name] })} />
      <UploadDrawer open={upload} repo={repo} dir={path} onClose={() => setUpload(false)} onDone={() => qc.invalidateQueries({ queryKey: ['browse', repo.name] })} />
    </>
  )
}

function useErrorTextStatic(e: unknown) {
  return e instanceof ApiError ? `${e.message} (${e.code})` : String(e)
}

function UploadDrawer({ open, repo, dir, onClose, onDone }: { open: boolean; repo: Repository; dir: string; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [file, setFile] = useState<File | null>(null)
  const [path, setPath] = useState('')
  const [overwrite, setOverwrite] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const pathTouched = useRef(false)
  useEffect(() => {
    if (file && !pathTouched.current) setPath((dir ? dir + '/' : '') + file.name)
  }, [file, dir])
  const wp = repo.attributes.hosted?.writePolicy ?? 'allow_once'
  const submit = async () => {
    if (!file) return
    setBusy(true)
    setError(null)
    const fd = new FormData()
    fd.append('file', file)
    fd.append('path', path)
    if (overwrite) fd.append('overwrite', 'true')
    try {
      await api(`repositories/${repo.name}/upload`, { method: 'POST', body: fd })
      message.success(t('browse.uploaded', 'Uploaded {{path}}', { path }))
      onDone()
      onClose()
      setFile(null)
      setPath('')
      pathTouched.current = false
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Drawer open={open} onClose={onClose} width={560} title={t('browse.uploadTo', 'Upload to {{name}}', { name: repo.name })} destroyOnClose>
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Upload.Dragger multiple={false} beforeUpload={(f) => { setFile(f); return false }} showUploadList={false} style={{ marginBottom: 16 }}>
        <p><CloudUploadOutlined style={{ fontSize: 28, color: 'var(--hlk-text-tertiary)' }} /></p>
        <p>{file ? <span className="hlk-mono">{file.name} · {fmtBytes(file.size)}</span> : t('browse.dropHint', 'Drop a file here or click to choose')}</p>
      </Upload.Dragger>
      <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 6 }}>{t('asset.path', 'Path')}</div>
      <Input className="hlk-mono" value={path} onChange={(e) => { pathTouched.current = true; setPath(e.target.value) }} placeholder="dir/file.ext" />
      <div style={{ marginTop: 12 }}>
        <Checkbox checked={overwrite} onChange={(e) => setOverwrite(e.target.checked)} disabled={wp !== 'allow'}>{t('browse.overwrite', 'Overwrite if the path exists')}</Checkbox>
        {wp === 'allow_once' && <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginTop: 4 }}>{t('browse.allowOnce', 'This repository does not allow redeploying an existing path (write policy: allow_once).')}</div>}
        {wp === 'deny' && <div style={{ fontSize: 12, color: 'var(--hlk-error)', marginTop: 4 }}>{t('browse.deny', 'This repository is read-only (write policy: deny).')}</div>}
      </div>
      {busy && <Progress percent={100} status="active" showInfo={false} style={{ marginTop: 12 }} />}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 20 }}>
        <Button onClick={onClose}>{t('common.cancel', 'Cancel')}</Button>
        <Button type="primary" disabled={!file || !path || wp === 'deny'} loading={busy} onClick={submit}>{t('browse.upload', 'Upload')}</Button>
      </div>
    </Drawer>
  )
}

export default function Browse() {
  const { t } = useTranslation()
  const params = useParams()
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const repoName = params.repo
  const path = params['*'] ?? ''
  const [filter, setFilter] = useState('')
  const [fmt, setFmt] = useState<string | null>(null)
  const [type, setType] = useState<string | null>(null)
  const [usage, setUsage] = useState(false)
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const list = useMemo(() => (repos.data ?? []).filter((r) => (!filter || r.name.toLowerCase().includes(filter.toLowerCase())) && (!fmt || r.format === fmt) && (!type || r.type === type)).sort((a, b) => a.name.localeCompare(b.name)), [repos.data, filter, fmt, type])
  const repo = repos.data?.find((r) => r.name === repoName)
  const formats = useMemo(() => Array.from(new Set((repos.data ?? []).map((r) => r.format))).sort(), [repos.data])
  useEffect(() => {
    if (sp.get('usage') === '1') setUsage(true)
  }, [sp])

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '280px minmax(0,1fr)', gap: 16, alignItems: 'start' }}>
      <div style={{ background: 'var(--hlk-card-alt)', border: '1px solid var(--hlk-border)', borderRadius: 10, padding: 10, position: 'sticky', top: 76, maxHeight: 'calc(100vh - 100px)', overflow: 'auto' }}>
        <Input size="small" prefix={<SearchOutlined />} placeholder={t('browse.filterRepos', 'Filter repositories')} value={filter} onChange={(e) => setFilter(e.target.value)} allowClear style={{ marginBottom: 8 }} />
        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginBottom: 8 }}>
          {['hosted', 'proxy', 'group'].map((ty) => (
            <Tag.CheckableTag key={ty} checked={type === ty} onChange={(c) => setType(c ? ty : null)} style={{ border: '1px solid var(--hlk-border)', fontSize: 11 }}>{t(`type.${ty}`, ty)}</Tag.CheckableTag>
          ))}
        </div>
        {formats.length > 1 && (
          <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginBottom: 8 }}>
            {formats.map((f) => (
              <Tag.CheckableTag key={f} checked={fmt === f} onChange={(c) => setFmt(c ? f : null)} style={{ border: '1px solid var(--hlk-border)', fontSize: 11, display: 'inline-flex', alignItems: 'center', gap: 4 }}><FormatIcon format={f} size={12} />{formatInfo(f).label}</Tag.CheckableTag>
            ))}
          </div>
        )}
        {repos.isLoading && <Skeleton active paragraph={{ rows: 6 }} />}
        {repos.isSuccess && list.length === 0 && <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', padding: 12 }}>{repos.data.length === 0 ? t('browse.noRepos', 'No repositories yet') : t('common.noMatch', 'No match')}</div>}
        {list.map((r) => (
          <div key={r.name} className={`hlk-list-row${r.name === repoName ? ' active' : ''}`} onClick={() => navigate(`/browse/${r.name}`)} style={{ opacity: r.online ? 1 : 0.6 }}>
            <FormatIcon format={r.format} size={20} />
            <div style={{ minWidth: 0, flex: 1 }}>
              <div className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.name}</div>
              <div style={{ fontSize: 10, display: 'flex', gap: 6 }}><TypeTag type={r.type} small />{!r.online && <TypeTag type="offline" small />}</div>
            </div>
            <span className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{fmtBytes(r.stats?.size)}</span>
          </div>
        ))}
      </div>
      <div>
        {!repo ? (
          <div className="hlk-card"><EmptyState title={repoName ? t('browse.notFound', 'Repository not found') : t('browse.pick', 'Pick a repository on the left')} hint={!repoName && can('app:repositories', 'write') && repos.data?.length === 0 ? t('browse.noReposHint', 'Create your first repository to start.') : undefined} action={!repoName && can('app:repositories', 'write') && repos.data?.length === 0 ? <Button type="primary" onClick={() => navigate('/admin/repositories/new')}>{t('repos.create', 'Create Repository')}</Button> : undefined} /></div>
        ) : (
          <div className="hlk-card">
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
              <FormatIcon format={repo.format} size={26} />
              <span className="hlk-mono" style={{ fontSize: 20, fontWeight: 600 }}>{repo.name}</span>
              <TypeTag type={repo.type} />
              {!repo.online && <TypeTag type="offline" />}
              <span style={{ border: '1px solid var(--hlk-border)', borderRadius: 6, padding: '3px 8px', background: 'var(--hlk-card)', fontSize: 12, minWidth: 0, maxWidth: 420 }}><Copyable text={repoBase(repo)} /></span>
              <div style={{ flex: 1 }} />
              <Button onClick={() => setUsage(true)}>{t('repos.usage', 'Usage')}</Button>
            </div>
            <RepoContent repo={repo} path={path} onNavigate={(p) => navigate(`/browse/${repo.name}${p ? '/' + p : ''}`)} />
            <Drawer open={usage} onClose={() => setUsage(false)} width={760} title={<span>{t('repos.usageTitle', 'How to use')} <span className="hlk-mono">{repo.name}</span></span>}>
              <UsageSnippets repo={repo} />
            </Drawer>
          </div>
        )}
      </div>
    </div>
  )
}
