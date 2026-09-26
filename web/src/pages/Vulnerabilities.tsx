import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Alert, App, Button, Checkbox, Input, Modal, Radio, Select, Table } from 'antd'
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

  // Export dialog. The levels start as whatever the page is filtered to —
  // the dashboard sends people here with "high and above" — but are chosen
  // explicitly, so nobody downloads a partial report without seeing it is.
  const [exportOpen, setExportOpen] = useState(false)
  const [exportAs, setExportAs] = useState('pdf')
  const [levels, setLevels] = useState<Severity[]>([])
  const [keepOther, setKeepOther] = useState(true)
  const allLevels: Severity[] = ['CRITICAL', 'HIGH', 'MODERATE', 'LOW', 'UNKNOWN']
  const rank = (x: Severity) => ({ CRITICAL: 4, HIGH: 3, MODERATE: 2, LOW: 1, UNKNOWN: 0 })[x]
  const openExport = () => {
    setLevels(allLevels.filter((x) => (level ? x === level : severity ? rank(x) >= rank(severity as Severity) : true)))
    setKeepOther(true)
    setExportOpen(true)
  }
  const otherFilters = [
    format && `${t('common.format', 'Format')}: ${formatInfo(format).label}`,
    repository && `${t('common.repository', 'Repository')}: ${repository}`,
    q && `${t('vulns.export.search', 'Search')}: "${q}"`,
  ].filter(Boolean) as string[]
  const doExport = () => {
    const p = new URLSearchParams({ as: exportAs, lang: i18n.language })
    if (levels.length < allLevels.length) p.set('levels', levels.join(','))
    if (keepOther) Object.entries({ repository, format, q }).forEach(([k, v]) => { if (v) p.set(k, v) })
    const a = document.createElement('a')
    a.href = `${API}/vulnerabilities/export?${p}`
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setExportOpen(false)
  }
  const exportFormats = [
    { key: 'pdf', label: 'PDF', hint: t('vulns.export.forPeople', 'to read and share') },
    { key: 'xlsx', label: 'Excel', hint: t('vulns.export.forPeople', 'to read and share') },
    { key: 'csv', label: 'CSV', hint: t('vulns.export.forTools', 'for scripts and AI agents') },
    { key: 'json', label: 'JSON', hint: t('vulns.export.forTools', 'for scripts and AI agents') },
  ]

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
            <Button icon={<DownloadOutlined />} onClick={openExport} disabled={!total}>{t('vulns.export.button', 'Export report')}</Button>
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
      <Modal
        open={exportOpen} onCancel={() => setExportOpen(false)} title={t('vulns.export.button', 'Export report')}
        okText={t('vulns.export.download', 'Download')} onOk={doExport} okButtonProps={{ disabled: levels.length === 0, icon: <DownloadOutlined /> }}
      >
        <div className="hlk-section-label" style={{ margin: '8px 0 8px' }}>{t('vulns.export.formatLabel', 'Format')}</div>
        <Radio.Group value={exportAs} onChange={(e) => setExportAs(e.target.value)} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          {exportFormats.map((f) => (
            <Radio key={f.key} value={f.key}><b>{f.label}</b> <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{f.hint}</span></Radio>
          ))}
        </Radio.Group>

        <div className="hlk-section-label" style={{ margin: '20px 0 8px', display: 'flex', justifyContent: 'space-between' }}>
          <span>{t('vulns.export.levels', 'Severities to include')}</span>
          <a style={{ textTransform: 'none', letterSpacing: 0 }} onClick={() => setLevels(levels.length === allLevels.length ? [] : allLevels)}>
            {levels.length === allLevels.length ? t('vulns.export.none', 'Clear') : t('vulns.export.all', 'Select all')}
          </a>
        </div>
        <Checkbox.Group value={levels} onChange={(v) => setLevels(allLevels.filter((x) => (v as Severity[]).includes(x)))} style={{ display: 'grid', gap: 8 }}>
          {allLevels.map((k) => (
            <Checkbox key={k} value={k}>
              <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
                <span style={{ display: 'inline-block', width: 88 }}><SeverityTag severity={k} /></span>
                <span style={{ fontVariantNumeric: 'tabular-nums', color: 'var(--hlk-text-secondary)' }}>{(s?.counts[k] ?? 0).toLocaleString()}</span>
              </span>
            </Checkbox>
          ))}
        </Checkbox.Group>
        {levels.length > 0 && levels.length < allLevels.length && (
          <Alert type="info" showIcon style={{ marginTop: 12 }} message={t('vulns.export.partial', 'The report will say which severities it leaves out, so it is not mistaken for the full list.')} />
        )}

        {otherFilters.length > 0 && (
          <>
            <div className="hlk-section-label" style={{ margin: '20px 0 8px' }}>{t('vulns.export.other', 'Other filters on this page')}</div>
            <Checkbox checked={keepOther} onChange={(e) => setKeepOther(e.target.checked)}>
              {t('vulns.export.keepOther', 'Apply them to the report too')}: <span className="hlk-mono" style={{ fontSize: 12 }}>{otherFilters.join(' · ')}</span>
            </Checkbox>
          </>
        )}
      </Modal>
      <PackageDrawer packageId={pkg} onClose={() => setPkg(null)} onAsset={(id) => setAsset(id)} />
      <AssetDrawer assetId={asset} onClose={() => setAsset(null)} />
    </>
  )
}
