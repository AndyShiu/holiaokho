import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Alert, App, Button, Skeleton, Tabs } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, put } from '@/api/client'
import type { CleanupPolicy, Repository, RoutingRule, Storage } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { FormatIcon } from '@/components/FormatIcon'
import { TypeTag } from '@/components/TypeTag'
import { Copyable } from '@/components/Copyable'
import { ConfirmDelete, useErrorText } from '@/components/Common'
import { fmtBytes, Num } from '@/components/Format'
import { RepoForm, fromRepo, type RepoFormValues } from '@/components/RepoForm'
import { UsageSnippets, repoBase } from '@/components/UsageSnippets'
import { RepoContent } from '@/pages/Browse'
import { assignedPolicies, invalidate, syncCleanup, toPayload } from './repoApi'

export default function RepoDetail() {
  const { t } = useTranslation()
  const { name = '', tab = 'settings' } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { can } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const storages = useQuery({ queryKey: ['storages'], queryFn: () => get<Storage[]>('storages') })
  const rules = useQuery({ queryKey: ['routing-rules'], queryFn: () => get<RoutingRule[]>('routing-rules') })
  const policies = useQuery({ queryKey: ['cleanup-policies'], queryFn: () => get<CleanupPolicy[]>('cleanup-policies') })
  const repo = repos.data?.find((r) => r.name === name)
  const [values, setValues] = useState<RepoFormValues | null>(null)
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [confirm, setConfirm] = useState(false)
  const [path, setPath] = useState('')
  useEffect(() => {
    if (repo && storages.data && policies.data && !dirty) setValues({ ...fromRepo(repo, storages.data), cleanupPolicies: assignedPolicies(repo.name, policies.data) })
  }, [repo, storages.data, policies.data, dirty])
  const canWrite = can('app:repositories', 'write')

  const save = async () => {
    if (!values || !repo) return
    setBusy(true)
    setError(null)
    try {
      await put(`repositories/${repo.name}`, toPayload(values, rules.data ?? []))
      await syncCleanup(repo.name, values.cleanupPolicies, policies.data ?? [])
      await qc.invalidateQueries({ queryKey: ['repositories'] })
      await qc.invalidateQueries({ queryKey: ['cleanup-policies'] })
      setDirty(false)
      message.success(t('common.saved', 'Saved'))
    } catch (e) {
      setError(e)
    } finally {
      setBusy(false)
    }
  }

  if (repos.isLoading) return <Skeleton active />
  if (!repo) return <Alert type="error" message={t('browse.notFound', 'Repository not found')} action={<Link to="/admin/repositories">{t('common.back', 'Back')}</Link>} />

  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
        <Link to="/admin/repositories" style={{ color: 'var(--hlk-text-tertiary)', fontSize: 13 }}>{t('nav.repositories', 'Repositories')} /</Link>
        <FormatIcon format={repo.format} size={28} />
        <span className="hlk-mono" style={{ fontSize: 22, fontWeight: 600 }}>{repo.name}</span>
        <TypeTag type={repo.type} />
        {!repo.online && <TypeTag type="offline" />}
        {repo.type === 'group' && <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{t('browse.groupHint', 'Merged from {{n}} members:', { n: repo.attributes.group?.members?.length ?? 0 })} {(repo.attributes.group?.members ?? []).map((m, i) => <span key={m}>{i > 0 && ', '}<Link to={`/admin/repositories/${m}`}>{m}</Link></span>)}</span>}
        <div style={{ flex: 1 }} />
        <span style={{ border: '1px solid var(--hlk-border)', borderRadius: 6, padding: '3px 8px', fontSize: 12, background: 'var(--hlk-card)' }}><Copyable text={repoBase(repo)} /></span>
      </div>
      <Tabs
        activeKey={tab} onChange={(k) => navigate(`/admin/repositories/${repo.name}/${k}`)}
        items={[
          {
            key: 'settings', label: t('repos.tab.settings', 'Settings'),
            children: values ? (
              <div style={{ maxWidth: 760 }}>
                {error ? <Alert type="error" showIcon message={errText(error)} style={{ marginBottom: 16 }} /> : null}
                <RepoForm format={repo.format} type={repo.type} value={values} onChange={(v) => { setValues(v); setDirty(true) }} editing existing={repo.name} />
                {canWrite && (
                  <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                    <Button type="primary" loading={busy} disabled={!dirty} onClick={save}>{t('common.save', 'Save')}</Button>
                    <Button disabled={!dirty} onClick={() => setDirty(false)}>{t('common.revert', 'Revert')}</Button>
                    {dirty && <span className="hlk-unsaved">● {t('common.unsaved', 'Unsaved changes')}</span>}
                  </div>
                )}
              </div>
            ) : <Skeleton active />,
          },
          { key: 'content', label: t('browse.content', 'Content'), children: <RepoContent repo={repo} path={path} onNavigate={setPath} /> },
          { key: 'usage', label: t('repos.usage', 'Usage'), children: <UsageSnippets repo={repo} /> },
          {
            key: 'stats', label: t('repos.tab.stats', 'Stats'),
            children: (
              <div style={{ maxWidth: 760 }}>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12, marginBottom: 24 }}>
                  {([[t('dashboard.packages', 'Packages'), <Num value={repo.stats?.packages} />], [t('dashboard.assets', 'Assets'), <Num value={repo.stats?.assets} />], [t('asset.size', 'Size'), fmtBytes(repo.stats?.size)]] as [React.ReactNode, React.ReactNode][]).map(([l, v], i) => (
                    <div key={i} className="hlk-card"><div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{l}</div><div style={{ fontSize: 28, fontWeight: 600 }}>{v}</div></div>
                  ))}
                </div>
                {(canWrite || can('app:repositories', 'delete')) && (
                  <div className="hlk-card" style={{ borderColor: 'var(--hlk-error)' }}>
                    <div className="hlk-section-label" style={{ color: 'var(--hlk-error)', marginBottom: 12 }}>{t('common.dangerZone', 'Danger zone')}</div>
                    <div style={{ display: 'flex', gap: 8 }}>
                      {repo.type !== 'hosted' && canWrite && <Button onClick={async () => { try { await invalidate(repo.name); message.success(t('repos.invalidated', 'Cache invalidated')) } catch (e) { message.error(errText(e)) } }}>{t('repos.invalidate', 'Invalidate cache')}</Button>}
                      {can('app:repositories', 'delete') && <Button danger onClick={() => setConfirm(true)}>{t('repos.deleteConfirm', 'Delete repository')}</Button>}
                    </div>
                  </div>
                )}
              </div>
            ),
          },
        ]}
      />
      <ConfirmDelete open={confirm} name={repo.name} title={<span>{t('repos.deleteTitle', 'Delete repository')} <span className="hlk-mono">{repo.name}</span>?</span>} description={t('repos.deleteCi', 'Clients using this URL will get 404.')} confirmLabel={t('repos.deleteConfirm', 'Delete repository')} onCancel={() => setConfirm(false)} loading={busy}
        onConfirm={async () => { setBusy(true); try { await del(`repositories/${repo.name}`); qc.invalidateQueries({ queryKey: ['repositories'] }); navigate('/admin/repositories') } catch (e) { message.error(errText(e)) } finally { setBusy(false) } }} />
    </>
  )
}
