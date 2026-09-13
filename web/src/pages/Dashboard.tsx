import { Link, useNavigate } from 'react-router-dom'
import { App, Button, Progress, Skeleton, Steps, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post } from '@/api/client'
import type { AuditEntry, Health, Repository, Task } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader } from '@/components/Common'
import { fmtBytes, Num, RelTime, StatusDot } from '@/components/Format'
import { useErrorText } from '@/components/Common'

function HealthCard({ name, c }: { name: string; c: Health['checks'][string] }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const pct = c.quotaBytes ? Math.min(100, Math.round(((c.usedBytes ?? 0) / c.quotaBytes) * 100)) : undefined
  const status = !c.healthy ? 'error' : c.message ? 'warning' : 'success'
  return (
    <div className="hlk-card" style={{ padding: '14px 16px', minHeight: 88, borderColor: !c.healthy ? 'var(--hlk-error)' : undefined }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span className="hlk-mono" style={{ fontSize: 13, fontWeight: 500 }}>{name}</span>
        <StatusDot status={status} />
      </div>
      {pct !== undefined && <Progress percent={pct} showInfo={false} size={['100%', 6]} strokeColor={pct >= 100 ? 'var(--hlk-error)' : pct >= 90 ? 'var(--hlk-warning)' : 'var(--hlk-ink)'} style={{ margin: '10px 0 4px' }} />}
      <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginTop: 6 }}>
        {c.usedBytes !== undefined ? `${fmtBytes(c.usedBytes)}${c.quotaBytes ? ` / ${fmtBytes(c.quotaBytes)}` : ''}` : c.message ?? (c.healthy ? t('health.ok', 'OK') : t('health.unhealthy', 'unhealthy'))}
        {c.usedBytes !== undefined && c.message && <div style={{ color: status === 'error' ? 'var(--hlk-error)' : 'var(--hlk-warning)' }}>{c.message}</div>}
      </div>
      {name === 'default_admin_password' && !c.healthy && (
        <Button size="small" danger style={{ marginTop: 8 }} onClick={() => navigate('/change-password?forced=1')}>{t('health.changeNow', 'Change now')}</Button>
      )}
    </div>
  )
}

function Stat({ label, value, sub }: { label: string; value: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <div className="hlk-card" style={{ padding: '14px 16px' }}>
      <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{label}</div>
      <div style={{ fontSize: 28, fontWeight: 600, letterSpacing: '-.02em', fontVariantNumeric: 'tabular-nums', lineHeight: 1.2, margin: '4px 0' }}>{value}</div>
      {sub && <div className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{sub}</div>}
    </div>
  )
}

export default function Dashboard() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const canStatus = can('app:status', 'read')
  const canRepos = can('app:repositories', 'read')
  const health = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check'), refetchInterval: 30000, enabled: canStatus })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories'), enabled: canRepos })
  const audit = useQuery({ queryKey: ['audit', 10], queryFn: () => get<AuditEntry[]>('audit', { limit: 10 }), enabled: can('app:system', 'read') })
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks'), enabled: can('app:tasks', 'read') })

  const checks = Object.entries(health.data?.checks ?? {}).sort(([a], [b]) => a.localeCompare(b))
  const bad = checks.filter(([, c]) => !c.healthy || c.message).length
  const repoList = repos.data ?? []
  const fresh = repos.isSuccess && repoList.length === 0
  const totals = repoList.reduce((a, r) => ({ packages: a.packages + (r.stats?.packages ?? 0), assets: a.assets + (r.stats?.assets ?? 0), size: a.size + (r.stats?.size ?? 0) }), { packages: 0, assets: 0, size: 0 })
  const byType = { hosted: repoList.filter((r) => r.type === 'hosted').length, proxy: repoList.filter((r) => r.type === 'proxy').length, group: repoList.filter((r) => r.type === 'group').length }
  const taskList = [...(tasks.data ?? [])].sort((a, b) => (a.lastStatus === 'failed' ? -1 : 0) - (b.lastStatus === 'failed' ? -1 : 0))

  const runTask = async (name: string) => {
    try {
      await post(`tasks/${name}/run`)
      message.success(t('tasks.started', 'Task {{name}} started', { name }))
      qc.invalidateQueries({ queryKey: ['tasks'] })
    } catch (e) {
      message.error(errText(e))
    }
  }

  if (!canStatus && !canRepos) {
    return (
      <>
        <PageHeader title={t('nav.dashboard', 'Dashboard')} />
        <div className="hlk-card">
          <p>{t('dashboard.devHint', 'Find packages in Browse or Search, and create a personal token for CI under My Tokens.')}</p>
          <Button type="primary" onClick={() => navigate('/browse')}>{t('nav.browse', 'Browse')}</Button>
        </div>
      </>
    )
  }

  return (
    <>
      <PageHeader
        title={t('nav.dashboard', 'Dashboard')}
        sub={
          canStatus && health.data && (
            <span style={{ display: 'inline-flex', gap: 10, alignItems: 'center' }}>
              <Tag color={bad ? 'warning' : 'success'} style={{ margin: 0 }}>{bad ? t('dashboard.attention', '{{n}} need attention', { n: bad }) : t('dashboard.allGood', 'All services healthy')}</Tag>
              <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{t('dashboard.refresh', 'refreshes every 30 s')}</span>
            </span>
          )
        }
        extra={can('app:repositories', 'write') && <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/admin/repositories/new')}>{t('repos.create', 'Create Repository')}</Button>}
      />
      {canStatus && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 12, marginBottom: 16 }}>
          {health.isLoading ? [0, 1, 2, 3].map((i) => <div key={i} className="hlk-card"><Skeleton active paragraph={{ rows: 1 }} /></div>) : checks.map(([k, c]) => <HealthCard key={k} name={k} c={c} />)}
        </div>
      )}
      {canRepos && fresh ? (
        <div className="hlk-card" style={{ marginBottom: 16 }}>
          <div style={{ fontWeight: 600, fontSize: 16, marginBottom: 4 }}>{t('dashboard.welcome', 'Welcome to Holiaokho')}</div>
          <div style={{ color: 'var(--hlk-text-secondary)', fontSize: 13, marginBottom: 20 }}>{t('dashboard.welcomeHint', 'Three steps to get your first repository serving.')}</div>
          <Steps
            current={health.data?.checks?.default_admin_password?.healthy ? 1 : 0}
            items={[
              { title: t('dashboard.step1', 'Change the admin password'), description: <Link to="/change-password">{t('dashboard.step1a', 'Change now')}</Link> },
              { title: t('dashboard.step2', 'Create your first proxy'), description: <Link to="/admin/repositories/new?format=maven&type=proxy">{t('dashboard.step2a', 'e.g. Maven Central')}</Link> },
              { title: t('dashboard.step3', 'Copy the client setup'), description: t('dashboard.step3a', 'from the repository "Usage" tab') },
            ]}
          />
        </div>
      ) : (
        canRepos && (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 12, marginBottom: 16 }}>
            <Stat label={t('dashboard.repos', 'Repositories')} value={<Num value={repoList.length} />} sub={`${byType.hosted} hosted · ${byType.proxy} proxy · ${byType.group} group`} />
            <Stat label={t('dashboard.packages', 'Packages')} value={<Num value={totals.packages} />} />
            <Stat label={t('dashboard.assets', 'Assets')} value={<Num value={totals.assets} />} />
            <Stat label={t('dashboard.size', 'Total size')} value={fmtBytes(totals.size)} sub={t('dashboard.dedup', 'content-addressed, deduplicated')} />
          </div>
        )
      )}
      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1.25fr) minmax(0,1fr)', gap: 12 }}>
        {audit.data && (
          <div className="hlk-card">
            <div className="hlk-section-label" style={{ marginBottom: 12 }}>{t('dashboard.activity', 'Recent activity')}</div>
            {audit.data.length === 0 && <div style={{ color: 'var(--hlk-text-tertiary)', fontSize: 13 }}>{t('common.nothingYet', 'Nothing yet')}</div>}
            {audit.data.map((a) => (
              <div key={a.id} style={{ display: 'grid', gridTemplateColumns: '80px 100px 1fr', gap: 10, fontSize: 13, padding: '7px 0', borderBottom: '1px solid var(--hlk-row)' }}>
                <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}><RelTime value={a.at} /></span>
                <span className="hlk-mono" style={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis' }}>{a.actor}</span>
                <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {t(`audit.${a.action}`, a.action)} <span className="hlk-mono" style={{ fontSize: 12 }}>{a.targetId}</span>
                </span>
              </div>
            ))}
            <div style={{ marginTop: 10 }}><Link to="/admin/system/audit" style={{ fontSize: 12 }}>{t('common.viewAll', 'View all →')}</Link></div>
          </div>
        )}
        {tasks.data && (
          <div className="hlk-card">
            <div className="hlk-section-label" style={{ marginBottom: 12 }}>{t('nav.tasks', 'Tasks')}</div>
            {taskList.map((x) => (
              <div key={x.name} style={{ display: 'grid', gridTemplateColumns: '8px 1fr auto auto', gap: 10, alignItems: 'center', fontSize: 13, padding: '7px 8px', margin: '0 -8px', borderBottom: '1px solid var(--hlk-row)', background: x.lastStatus === 'failed' ? 'var(--hlk-error-bg)' : undefined, borderRadius: 4 }}>
                <StatusDot status={x.running ? 'running' : x.lastStatus === 'failed' ? 'error' : x.lastStatus === 'success' ? 'success' : 'idle'} />
                <div style={{ minWidth: 0 }}>
                  <div>{t(`tasks.name.${x.name}`, x.name)}</div>
                  <div className="hlk-mono" style={{ fontSize: 11, color: x.lastStatus === 'failed' ? 'var(--hlk-error)' : 'var(--hlk-text-tertiary)' }}>{x.lastStatus ?? '—'}{x.lastRun ? ' · ' : ''}{x.lastRun && <RelTime value={x.lastRun} />}</div>
                </div>
                <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{x.nextRun ? <RelTime value={x.nextRun} /> : '—'}</span>
                {can('app:tasks', 'write') && <a onClick={() => runTask(x.name)} style={{ fontSize: 12 }}>{t('tasks.runNow', 'Run now')}</a>}
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
