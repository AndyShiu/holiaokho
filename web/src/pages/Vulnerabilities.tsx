import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Alert, App, Button, Dropdown, Input, Select, Table } from 'antd'
import { DownloadOutlined, SearchOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { API, get, post } from '@/api/client'
import type { Repository, Severity, VulnFinding, VulnSummary } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { EmptyState, PageHeader, useErrorText } from '@/components/Common'
import { AssetDrawer, PackageDrawer } from '@/components/Drawers'
import { FormatIcon } from '@/components/FormatIcon'
import { Num, RelTime } from '@/components/Format'
import { SeverityTag, VulnId, packageLabel, severities } from '@/components/Vulns'
import { formatInfo } from '@/theme/tokens'

const PAGE = 50

export default function Vulnerabilities() {
  const { t, i18n } = useTranslation()
  const { can } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const qc = useQueryClient()
  const [sp, setSp] = useSearchParams()
  // Two filters on purpose. "severity" is a floor — HIGH means high and
  // worse, which is what the dashboard sends you to. "level" is exact, for
  // the count cards: the card says 3, clicking it shows those 3.
  const severity = sp.get('severity') ?? ''
  const level = sp.get('level') ?? ''
  const repository = sp.get('repository') ?? ''
  const format = sp.get('format') ?? ''
  const q = sp.get('q') ?? ''
  const [input, setInput] = useState(q)
  const [page, setPage] = useState(0)
  const [pkg, setPkg] = useState<string | null>(null)
  const [asset, setAsset] = useState<string | null>(null)

  const summary = useQuery({ queryKey: ['vuln-summary'], queryFn: () => get<VulnSummary>('vulnerabilities/summary') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const list = useQuery({
    queryKey: ['vulns', severity, level, repository, format, q, page],
    queryFn: () => get<{ items: VulnFinding[]; total: number }>('vulnerabilities', { severity, level, repository, format, q, limit: PAGE, offset: page * PAGE }),
  })
  useEffect(() => setInput(q), [q])
  useEffect(() => {
    const id = setTimeout(() => { if (input !== q) update({ q: input }) }, 400)
    return () => clearTimeout(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input])
  const update = (patch: Record<string, string>) => {
    const next = new URLSearchParams(sp)
    Object.entries(patch).forEach(([k, v]) => (v ? next.set(k, v) : next.delete(k)))
    setSp(next, { replace: true })
    setPage(0)
  }

  const s = summary.data
  const covered = useMemo(() => new Set(s?.coveredFormats ?? []), [s])
  const repoOptions = useMemo(() => (repos.data ?? [])
    .filter((r) => r.type !== 'group' && covered.has(r.format) && (!format || r.format === format))
    .map((r) => ({ value: r.name, label: r.name })), [repos.data, covered, format])
  const total = severities.reduce((n, k) => n + (s?.counts[k] ?? 0), 0)

  // The report covers what the page is showing: the same filters, in the
  // language the page is in. The browser downloads it with the session it
  // already has.
  const exportAs = (as: string) => {
    const p = new URLSearchParams({ as, lang: i18n.language })
    Object.entries({ severity, level, repository, format, q }).forEach(([k, v]) => { if (v) p.set(k, v) })
    const a = document.createElement('a')
    a.href = `${API}/vulnerabilities/export?${p}`
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    a.remove()
  }
  const exportItems = [
    { key: 'pdf', label: 'PDF', hint: t('vulns.export.forPeople', 'to read and share') },
    { key: 'xlsx', label: 'Excel', hint: t('vulns.export.forPeople', 'to read and share') },
    { key: 'csv', label: 'CSV', hint: t('vulns.export.forTools', 'for scripts and AI agents') },
    { key: 'json', label: 'JSON', hint: t('vulns.export.forTools', 'for scripts and AI agents') },
  ].map((x) => ({
    key: x.key,
    label: <span style={{ display: 'inline-flex', gap: 12, justifyContent: 'space-between', minWidth: 190 }}><b>{x.label}</b><span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{x.hint}</span></span>,
    onClick: () => exportAs(x.key),
  }))

  const scanNow = async () => {
    try {
      await post('tasks/scan-vulnerabilities/run')
      message.success(t('vulns.scanStarted', 'Scan started. Results appear here as it finishes.'))
      setTimeout(() => { qc.invalidateQueries({ queryKey: ['vuln-summary'] }); qc.invalidateQueries({ queryKey: ['vulns'] }) }, 5000)
    } catch (e) {
      message.error(errText(e))
    }
  }

  return (
    <>
      <PageHeader
        title={t('vulns.title', 'Vulnerabilities')}
        sub={s?.lastOkAt && <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{t('vulns.lastScan', 'Last checked')} <RelTime value={s.lastOkAt} /> · {t('vulns.source', 'data from OSV.dev')}</span>}
        extra={
          <span style={{ display: 'inline-flex', gap: 8 }}>
            <Dropdown menu={{ items: exportItems }} trigger={['click']} placement="bottomRight" disabled={!list.data?.total}>
              <Button icon={<DownloadOutlined />}>{t('vulns.export.button', 'Export report')}</Button>
            </Dropdown>
            {can('app:tasks', 'write') && s?.enabled && <Button onClick={scanNow}>{t('vulns.scanNow', 'Scan now')}</Button>}
          </span>
        }
      />

      {s && !s.enabled && (
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('vulns.disabled', 'Vulnerability scanning is turned off in the server configuration (vulnerabilities.enabled).')} />
      )}
      {s?.lastError && (
        <Alert
          type="warning" showIcon style={{ marginBottom: 16 }}
          message={t('vulns.unreachable', 'The last scan could not reach OSV, so these results may be out of date.')}
          description={<span className="hlk-mono" style={{ fontSize: 12 }}>{s.lastError}</span>}
        />
      )}

      {/* The counts double as filters: one click to see just the criticals. */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 12, marginBottom: 12 }}>
        {(['CRITICAL', 'HIGH', 'MODERATE', 'LOW'] as Severity[]).map((k) => {
          const on = level === k
          return (
            <button
              key={k} type="button" className={`hlk-card hlk-sevcard${on ? ' on' : ''}`} onClick={() => update({ level: on ? '' : k, severity: '' })}
            >
              <SeverityTag severity={k} />
              <div style={{ fontSize: 26, fontWeight: 600, fontVariantNumeric: 'tabular-nums', marginTop: 6 }}><Num value={s?.counts[k] ?? 0} /></div>
            </button>
          )
        })}
      </div>
      {s && (
        // Coverage next to the counts, always: zero findings means little
        // without knowing how much was actually looked at.
        <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginBottom: 16, display: 'flex', gap: 14, flexWrap: 'wrap' }}>
          <span>{t('vulns.cov.scanned', '{{n}} packages checked', { n: s.scanned })}</span>
          {s.pending > 0 && <span>{t('vulns.cov.pending', '{{n}} waiting to be checked', { n: s.pending })}</span>}
          {s.notCovered > 0 && <span>{t('vulns.cov.notCovered', '{{n}} not covered by OSV (not the same as safe)', { n: s.notCovered })}</span>}
          {s.excluded > 0 && <span>{t('vulns.cov.excluded', '{{n}} in repositories with scanning off', { n: s.excluded })}</span>}
        </div>
      )}

      <div className="hlk-card" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Input prefix={<SearchOutlined />} placeholder={t('vulns.searchPlaceholder', 'Package, GHSA or CVE id')} value={input} onChange={(e) => setInput(e.target.value)} onPressEnter={() => update({ q: input })} allowClear style={{ width: 280 }} />
          <Select allowClear placeholder={t('vulns.minSeverity', 'Severity (at least)')} value={severity || undefined} onChange={(v) => update({ severity: v ?? '', level: '' })} style={{ width: 180 }}
            options={(['CRITICAL', 'HIGH', 'MODERATE', 'LOW'] as Severity[]).map((k) => ({ value: k, label: t(`vulns.sev.${k}`, k) }))} />
          <Select allowClear placeholder={t('common.format', 'Format')} value={format || undefined} onChange={(v) => update({ format: v ?? '', repository: '' })} style={{ width: 170 }}
            options={[...covered].sort().map((f) => ({ value: f, label: <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><FormatIcon format={f} size={14} />{formatInfo(f).label}</span> }))} />
          <Select allowClear showSearch placeholder={t('common.repository', 'Repository')} value={repository || undefined} onChange={(v) => update({ repository: v ?? '' })} style={{ width: 220 }} options={repoOptions} />
        </div>
      </div>

      <div className="hlk-card" style={{ padding: 0 }}>
        <Table<VulnFinding>
          rowKey={(r) => `${r.packageId}:${r.id}`} loading={list.isLoading} dataSource={list.data?.items ?? []} className="hlk-table hlk-clickable" scroll={{ x: 1000 }} size="middle"
          onRow={(r) => ({ onClick: () => setPkg(String(r.packageId)) })}
          locale={{
            emptyText: (
              <EmptyState
                title={total === 0 && s && s.scanned > 0 ? t('vulns.noneTitle', 'No known vulnerabilities') : t('vulns.noMatch', 'Nothing matches these filters')}
                hint={total === 0 && s && s.scanned > 0 ? t('vulns.noneHint', 'Among the {{n}} packages checked. Formats OSV does not cover are not included.', { n: s.scanned }) : undefined}
              />
            ),
          }}
          pagination={{ current: page + 1, pageSize: PAGE, total: list.data?.total ?? 0, onChange: (p) => setPage(p - 1), showSizeChanger: false, showTotal: (n) => t('vulns.total', '{{n}} findings', { n }) }}
          columns={[
            { title: t('vulns.severity', 'Severity'), dataIndex: 'severity', width: 130, render: (_: unknown, r) => <SeverityTag severity={r.severity} score={r.score} /> },
            { title: t('vulns.id', 'Vulnerability'), dataIndex: 'id', width: 190, render: (_: unknown, r) => <span onClick={(e) => e.stopPropagation()}><VulnId f={r} /></span> },
            {
              title: t('vulns.package', 'Package'), render: (_: unknown, r) => (
                <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', minWidth: 0 }}>
                  <FormatIcon format={r.format} size={16} />
                  <span className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500 }}>{packageLabel(r)}</span>
                  <span className="hlk-mono" style={{ fontSize: 12 }}>{r.version}</span>
                </span>
              ),
            },
            { title: t('common.repository', 'Repository'), dataIndex: 'repository', width: 160, render: (x: string) => <Link to={`/browse/${x}`} onClick={(e) => e.stopPropagation()} className="hlk-mono" style={{ fontSize: 12 }}>{x}</Link> },
            { title: t('vulns.summary', 'Summary'), dataIndex: 'summary', render: (x: string) => <span style={{ fontSize: 12.5 }}>{x || '—'}</span> },
            { title: t('vulns.fixedIn', 'Fixed in'), dataIndex: 'fixedIn', width: 150, render: (x: string[]) => x.length ? <span className="hlk-mono" style={{ fontSize: 12 }}>{x.join(', ')}</span> : <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{t('vulns.noFixShort', 'none yet')}</span> },
            { title: t('vulns.firstSeen', 'Found'), dataIndex: 'firstSeen', width: 110, render: (x: string) => <RelTime value={x} /> },
          ]}
        />
      </div>
      <PackageDrawer packageId={pkg} onClose={() => setPkg(null)} onAsset={(id) => setAsset(id)} />
      <AssetDrawer assetId={asset} onClose={() => setAsset(null)} />
    </>
  )
}
