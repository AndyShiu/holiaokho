import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { App, Button, Input, Segmented, Select, Skeleton, Switch, Tag } from 'antd'
import { CopyOutlined, DownloadOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, put } from '@/api/client'
import type { AuditEntry, Health } from '@/api/types'
import { KV, PageHeader, useErrorText } from '@/components/Common'
import { copyText } from '@/components/Copyable'
import { AbsTime, fmtBytes, StatusDot } from '@/components/Format'
import { FormatIcon } from '@/components/FormatIcon'
import { SortableTable } from '@/components/SortableTable'

function HealthTab() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check'), refetchInterval: 30000 })
  const rows = Object.entries(q.data?.checks ?? {}).map(([k, c]) => ({ key: k, ...c }))
  // Derive the headline from the checks themselves: the server's top-level
  // flag ignores advisory checks such as the default admin password.
  const failed = rows.filter((r) => !r.healthy).length
  const warned = rows.filter((r) => r.healthy && r.message).length
  const allGood = failed === 0 && warned === 0
  return (
    <div className="hlk-card" style={{ padding: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '12px 16px' }}>
        <StatusDot status={failed ? 'error' : warned ? 'warning' : 'success'} /><span style={{ fontWeight: 500 }}>{allGood ? t('dashboard.allGood', 'All services healthy') : t('dashboard.attention', '{{n}} need attention', { n: failed + warned })}</span>
        <div style={{ flex: 1 }} /><Button size="small" icon={<ReloadOutlined />} onClick={() => qc.invalidateQueries({ queryKey: ['health'] })}>{t('health.recheck', 'Re-check')}</Button>
      </div>
      <SortableTable rowKey="key" dataSource={rows} pagination={false} size="middle" className="hlk-table" loading={q.isLoading} columns={[
        { title: t('health.check', 'Check'), dataIndex: 'key', render: (x: string) => <span className="hlk-mono">{x}</span> },
        { title: t('common.status', 'Status'), sortValue: (r: any) => r.healthy, width: 120, render: (_: unknown, r: any) => <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}><StatusDot status={!r.healthy ? 'error' : r.message ? 'warning' : 'success'} />{r.healthy ? 'OK' : t('health.unhealthy', 'unhealthy')}</span> },
        { title: t('health.message', 'Message'), sortValue: (r: any) => r.message, render: (_: unknown, r: any) => (r.code ? t(`health.msg.${r.code}`, { defaultValue: r.message }) : r.message) || '—' },
        { title: t('storages.usage', 'Usage'), sortValue: (r: any) => r.usedBytes, width: 220, render: (_: unknown, r: any) => (r.usedBytes !== undefined ? `${fmtBytes(r.usedBytes)}${r.quotaBytes ? ` / ${fmtBytes(r.quotaBytes)}` : ''}` : '') },
      ]} />
    </div>
  )
}

function InfoTab() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const q = useQuery({ queryKey: ['system-info'], queryFn: () => get<any>('system/info') })
  if (!q.data) return <Skeleton active />
  const d = q.data
  const mem = d.memory ?? {}
  const group = (title: string, items: [string, any][]) => (
    <div className="hlk-card"><div className="hlk-section-label" style={{ marginBottom: 12 }}>{title}</div><KV labelWidth={140} items={items.map(([k, v]) => [k, <span className="hlk-mono" style={{ fontSize: 12 }}>{typeof v === 'object' ? JSON.stringify(v) : String(v ?? '—')}</span>])} /></div>
  )
  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}><Button size="small" icon={<CopyOutlined />} onClick={async () => { await copyText(JSON.stringify(d, null, 2)); message.success(t('common.copied', 'Copied')) }}>{t('info.copy', 'Copy as text')}</Button></div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16 }}>
        {group(t('info.version', 'Version'), [['version', d.version], ['go', d.go], ['os / arch', `${d.os} / ${d.arch}`]])}
        {group(t('info.runtime', 'Runtime'), [['hostname', d.hostname], ['pid', d.pid], ['uptime', d.uptime], ['cpus', d.cpus], ['goroutines', d.goroutines], ['heap', fmtBytes(mem.heapAllocBytes)], ['sys', fmtBytes(mem.sysBytes)], ['numGC', mem.numGC]])}
        {group(t('info.database', 'Database'), Object.entries(d.database ?? {}))}
        <div className="hlk-card"><div className="hlk-section-label" style={{ marginBottom: 12 }}>Formats · {d.formats?.length}</div><div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>{(d.formats ?? []).map((f: string) => <Tag key={f} style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><FormatIcon format={f} size={14} />{f}</Tag>)}</div></div>
        {Array.isArray(d.storages) && <div className="hlk-card" style={{ gridColumn: '1 / -1' }}><div className="hlk-section-label" style={{ marginBottom: 12 }}>{t('nav.storages', 'Storages')}</div><SortableTable size="small" pagination={false} rowKey={(r: any) => r.name ?? JSON.stringify(r)} dataSource={d.storages} columns={Object.keys(d.storages[0] ?? {}).map((k) => ({ title: k, dataIndex: k, render: (v: any) => <span className="hlk-mono" style={{ fontSize: 12 }}>{typeof v === 'number' && k.toLowerCase().includes('bytes') ? fmtBytes(v) : typeof v === 'object' ? JSON.stringify(v) : String(v)}</span> }))} /></div>}
      </div>
    </>
  )
}

const LEVELS = ['ALL', 'ERROR', 'WARN', 'INFO', 'DEBUG']
const levelColor: Record<string, string> = { ERROR: 'var(--hlk-error)', WARN: 'var(--hlk-warning)', INFO: 'var(--hlk-text-secondary)', DEBUG: 'var(--hlk-text-tertiary)' }
function parseLine(l: string) {
  const m = l.match(/time=(\S+)\s+level=(\w+)\s+msg="?(.*?)"?(\s+\w+=|$)/) || l.match(/^(\S+)\s+(ERROR|WARN|INFO|DEBUG)\s+(.*)$/)
  if (!m) return { ts: '', level: 'INFO', msg: l }
  return { ts: m[1], level: m[2].toUpperCase(), msg: l.slice(l.indexOf('msg=') >= 0 ? l.indexOf('msg=') + 4 : 0) }
}

function LogsTab() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [tail, setTail] = useState(500)
  const [level, setLevel] = useState('ALL')
  const [kw, setKw] = useState('')
  const [auto, setAuto] = useState(false)
  const [wrap, setWrap] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const q = useQuery({ queryKey: ['logs', tail], queryFn: () => get<string[]>('system/logs', { tail }), refetchInterval: auto ? 2000 : false })
  const lvl = useQuery({ queryKey: ['log-level'], queryFn: () => get<{ level: string }>('system/log-level') })
  const lines = useMemo(() => (q.data ?? []).map(parseLine).filter((x) => (level === 'ALL' || x.level === level) && (!kw || x.msg.toLowerCase().includes(kw.toLowerCase()))), [q.data, level, kw])
  useEffect(() => { if (auto && box.current) box.current.scrollTop = box.current.scrollHeight }, [lines, auto])
  return (
    <>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap', marginBottom: 12 }}>
        <Select size="small" value={tail} onChange={setTail} options={[200, 500, 2000].map((n) => ({ value: n, label: `tail ${n}` }))} />
        <Segmented size="small" value={level} onChange={(v) => setLevel(String(v))} options={LEVELS.map((l) => ({ value: l, label: <span className="hlk-mono" style={{ fontSize: 11, color: l !== 'ALL' && level !== l ? levelColor[l] : undefined }}>{l}</span> }))} />
        <Input size="small" placeholder={t('logs.filter', 'Filter')} value={kw} onChange={(e) => setKw(e.target.value)} allowClear style={{ width: 260 }} />
        <span style={{ fontSize: 12, display: 'inline-flex', gap: 6, alignItems: 'center' }}><Switch size="small" checked={auto} onChange={setAuto} />{t('logs.auto', 'Auto refresh')}</span>
        <span style={{ fontSize: 12, display: 'inline-flex', gap: 6, alignItems: 'center' }}><Switch size="small" checked={wrap} onChange={setWrap} />{t('logs.wrap', 'Wrap')}</span>
        <div style={{ flex: 1 }} />
        <span style={{ fontSize: 12 }}>{t('logs.level', 'Log level')}</span>
        <Select size="small" value={lvl.data?.level} onChange={async (v) => { try { await put('system/log-level', { level: v }); lvl.refetch(); message.success(t('logs.levelSet', 'Log level set to {{v}} (resets on restart)', { v })) } catch (e) { message.error(errText(e)) } }} options={['debug', 'info', 'warn', 'error'].map((l) => ({ value: l }))} style={{ width: 100 }} />
        <Button size="small" icon={<CopyOutlined />} onClick={async () => { await copyText((q.data ?? []).join('\n')); message.success(t('common.copied', 'Copied')) }}>{t('logs.copyAll', 'Copy all')}</Button>
      </div>
      <div ref={box} className="hlk-logbox" style={{ height: 'calc(100vh - 280px)', minHeight: 300 }}>
        {lines.map((l, i) => (
          <div key={i} style={{ display: 'grid', gridTemplateColumns: '190px 56px 1fr', gap: 8, background: l.level === 'ERROR' ? 'rgba(229,107,107,.08)' : undefined }}>
            <span style={{ color: '#6F7A8A', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.ts}</span>
            <span style={{ color: levelColor[l.level] ?? '#9AA5B5', fontWeight: 500 }}>{l.level}</span>
            <span style={{ whiteSpace: wrap ? 'pre-wrap' : 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{l.msg}</span>
          </div>
        ))}
        {auto && <div style={{ color: 'var(--hlk-success)', marginTop: 6 }}>● tailing…</div>}
      </div>
    </>
  )
}

function ConfigTab() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['system-config'], queryFn: () => get<any>('system/config') })
  return (
    <>
      <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginBottom: 12 }}>{t('config.hint', 'Read-only. Secrets are masked. Change values in the config file or with HOLIAOKHO_* environment variables, then restart.')}</div>
      <pre className="hlk-logbox" style={{ maxHeight: 'calc(100vh - 260px)' }}>{q.data ? JSON.stringify(q.data, null, 2) : '…'}</pre>
    </>
  )
}

function SupportTab() {
  const { t } = useTranslation()
  return (
    <div className="hlk-card" style={{ maxWidth: 640 }}>
      <p style={{ marginTop: 0 }}>{t('support.hint', 'A zip with everything needed to diagnose a problem, without user data or blobs:')}</p>
      <ul style={{ fontSize: 13, lineHeight: 1.9 }}>
        <li>{t('support.i1', 'system information and health checks')}</li>
        <li>{t('support.i2', 'configuration (secrets masked)')}</li>
        <li>{t('support.i3', 'recent log lines')}</li>
        <li>{t('support.i4', 'repository, storage and task definitions')}</li>
      </ul>
      <Button type="primary" icon={<DownloadOutlined />} href="/api/v1/system/support-zip">{t('support.download', 'Download support zip')}</Button>
    </div>
  )
}

function AuditTab() {
  const { t } = useTranslation()
  const [limit, setLimit] = useState(100)
  const [actor, setActor] = useState('')
  const [action, setAction] = useState<string | undefined>()
  const q = useQuery({ queryKey: ['audit', limit], queryFn: () => get<AuditEntry[]>('audit', { limit }) })
  const actions = useMemo(() => Array.from(new Set((q.data ?? []).map((a) => a.action))).sort(), [q.data])
  const rows = (q.data ?? []).filter((a) => (!actor || a.actor.includes(actor)) && (!action || a.action === action))
  return (
    <div className="hlk-card" style={{ padding: 0 }}>
      <div style={{ display: 'flex', gap: 8, padding: '12px 16px', alignItems: 'center' }}>
        <Input size="small" placeholder="actor" value={actor} onChange={(e) => setActor(e.target.value)} allowClear style={{ width: 160 }} />
        <Select size="small" allowClear placeholder="action" value={action} onChange={setAction} options={actions.map((a) => ({ value: a }))} style={{ width: 220 }} className="hlk-mono" />
        <div style={{ flex: 1 }} />
        <Select size="small" value={limit} onChange={setLimit} options={[100, 500, 2000].map((n) => ({ value: n, label: `${t('audit.last', 'last')} ${n}` }))} />
      </div>
      <SortableTable<AuditEntry>
        rowKey="id" loading={q.isLoading} dataSource={rows} size="small" className="hlk-table" pagination={{ pageSize: 50, showSizeChanger: false }}
        expandable={{ expandedRowRender: (r) => <pre className="hlk-logbox" style={{ margin: 0, fontSize: 11 }}>{JSON.stringify(r.detail, null, 2)}</pre>, rowExpandable: (r) => !!r.detail }}
        columns={[
          { title: t('common.time', 'Time'), dataIndex: 'at', width: 180, render: (x: string) => <AbsTime value={x} /> },
          { title: 'Actor', dataIndex: 'actor', width: 160, render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
          { title: 'Action', dataIndex: 'action', width: 220, render: (x: string) => <span>{t(`audit.${x}`, x)} <span className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{x}</span></span> },
          { title: 'Target', render: (_: unknown, r) => <span className="hlk-mono" style={{ fontSize: 12 }}><span style={{ color: 'var(--hlk-text-tertiary)' }}>{r.targetType}</span> {r.targetId}</span> },
        ]}
      />
    </div>
  )
}

export default function System() {
  const { t } = useTranslation()
  const { tab = 'health' } = useParams()
  const navigate = useNavigate()
  const tabs = [
    { key: 'health', label: t('system.health', 'Health') }, { key: 'info', label: t('system.info', 'Information') }, { key: 'logs', label: 'Logs' },
    { key: 'config', label: t('system.config', 'Configuration') }, { key: 'support', label: 'Support ZIP' }, { key: 'audit', label: t('system.audit', 'Audit Log') },
  ]
  return (
    <>
      <PageHeader title={t('nav.system', 'System')} />
      <Segmented value={tab} onChange={(v) => navigate(`/admin/system/${v}`)} options={tabs.map((x) => ({ value: x.key, label: x.label }))} style={{ marginBottom: 16 }} />
      {tab === 'health' && <HealthTab />}
      {tab === 'info' && <InfoTab />}
      {tab === 'logs' && <LogsTab />}
      {tab === 'config' && <ConfigTab />}
      {tab === 'support' && <SupportTab />}
      {tab === 'audit' && <AuditTab />}
    </>
  )
}
