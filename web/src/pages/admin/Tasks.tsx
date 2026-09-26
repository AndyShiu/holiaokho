import { useState } from 'react'
import { Alert, App, Button, Input, Modal, Segmented, Select, Switch, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import dayjs from 'dayjs'
import { get, post, put } from '@/api/client'
import type { Task, TaskRun } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, useErrorText } from '@/components/Common'
import { AbsTime, RelTime, StatusDot } from '@/components/Format'
import { SortableTable } from '@/components/SortableTable'

export function describeCron(cron: string, t: (k: string, d: string, o?: any) => string): string {
  const f = cron.trim().split(/\s+/)
  if (f.length !== 5) return t('cron.invalid', 'invalid expression')
  const [mi, ho, dm, mo, dw] = f
  const time = /^\d+$/.test(mi) && /^\d+$/.test(ho) ? `${ho.padStart(2, '0')}:${mi.padStart(2, '0')}` : null
  if (mi.startsWith('*/') && ho === '*') return t('cron.everyNMin', 'every {{n}} minutes', { n: mi.slice(2) })
  if (mi === '0' && ho === '*') return t('cron.hourly', 'every hour')
  if (time && dm === '*' && mo === '*' && dw === '*') return t('cron.daily', 'every day at {{time}}', { time })
  if (time && dm === '*' && mo === '*' && dw !== '*') return t('cron.weekly', 'every week on day {{dow}} at {{time}}', { dow: dw, time })
  if (time && dm !== '*' && mo === '*') return t('cron.monthly', 'day {{dom}} of every month at {{time}}', { dom: dm, time })
  return cron
}

export default function Tasks() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [sched, setSched] = useState<Task | null>(null)
  const [mode, setMode] = useState<'default' | 'cron'>('default')
  const [cron, setCron] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [filterTask, setFilterTask] = useState<string | undefined>()
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks'), refetchInterval: (q) => (q.state.data?.some((x) => x.running) ? 3000 : 30000) })
  const runs = useQuery({ queryKey: ['task-runs', filterTask], queryFn: () => get<TaskRun[]>('tasks/runs', { task: filterTask }), refetchInterval: tasks.data?.some((x) => x.running) ? 3000 : false })
  const canWrite = can('app:tasks', 'write')
  const run = useMutation({ mutationFn: (name: string) => post(`tasks/${name}/run`), onSuccess: (_, name) => { message.success(t('tasks.started', 'Task {{name}} started', { name })); qc.invalidateQueries({ queryKey: ['tasks'] }); qc.invalidateQueries({ queryKey: ['task-runs'] }) }, onError: (e) => message.error(errText(e)) })
  const save = useMutation({ mutationFn: () => put(`tasks/${sched!.name}/schedule`, { cron: mode === 'cron' ? cron : '', enabled }), onSuccess: () => { message.success(t('common.saved', 'Saved')); setSched(null); qc.invalidateQueries({ queryKey: ['tasks'] }) }, onError: (e) => message.error(errText(e)) })
  const toggle = useMutation({ mutationFn: (x: Task) => put(`tasks/${x.name}/schedule`, { cron: x.cron ?? '', enabled: !x.enabled }), onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }), onError: (e) => message.error(errText(e)) })
  const running = tasks.data?.filter((x) => x.running).length ?? 0
  // Cron runs on the server's clock, so the preview comes from the server:
  // its own parser, in its own zone, shown here in the viewer's time. The
  // browser used to work it out locally and show "0 2 * * *" as 02:00 here
  // while the server ran it at 02:00 UTC.
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<{ timeZone?: string; utcOffsetMinutes?: number }>('status') })
  const serverOffset = status.data?.utcOffsetMinutes ?? 0
  const localOffset = -new Date().getTimezoneOffset()
  const serverZone = status.data?.timeZone ? `${status.data.timeZone} (UTC${serverOffset >= 0 ? '+' : '-'}${Math.abs(serverOffset / 60)})` : ''
  const cronPreview = useQuery({
    queryKey: ['cron-preview', cron], enabled: mode === 'cron' && !!cron.trim(), staleTime: 60000,
    queryFn: () => get<{ valid: boolean; next?: string[]; error?: string }>('tasks/cron-preview', { expr: cron }),
  })
  const preview = cronPreview.data?.valid ? cronPreview.data.next ?? [] : null
  const serverTime = (iso: string) => dayjs(iso).add(serverOffset - localOffset, 'minute').format('HH:mm')
  const templates = [{ l: t('cron.tplDaily', 'Daily 02:00'), v: '0 2 * * *' }, { l: t('cron.tplHourly', 'Hourly'), v: '0 * * * *' }, { l: t('cron.tplSunday', 'Sunday 03:00'), v: '0 3 * * 0' }]
  const dur = (r: TaskRun) => (r.finishedAt ? `${((new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime()) / 1000).toFixed(1)} s` : '…')

  return (
    <>
      <PageHeader title={t('nav.tasks', 'Tasks')} sub={<span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}><StatusDot status={running ? 'running' : 'idle'} />{running ? t('tasks.running', '{{n}} running · refreshes every 3 s', { n: running }) : t('tasks.idle', 'idle')}</span>} />
      <div className="hlk-card" style={{ padding: 0, marginBottom: 16 }}>
        <SortableTable<Task>
          rowKey="name" loading={tasks.isLoading} dataSource={tasks.data ?? []} className="hlk-table" scroll={{ x: 1120 }} pagination={false} size="middle"
          columns={[
            { title: t('tasks.task', 'Task'), dataIndex: 'name', width: 190, render: (n: string) => <div><div style={{ fontWeight: 500 }}>{t(`tasks.name.${n}`, n)}</div><div className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{n}</div></div> },
            { title: t('common.description', 'Description'), dataIndex: 'description', render: (x: string, r) => <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{t(`tasks.desc.${r.name}`, { defaultValue: x })}</span> },
            { title: t('tasks.schedule', 'Schedule'), sortValue: (x: any) => x.cron || x.interval, width: 200, render: (_: unknown, x) => x.cron ? <div><div className="hlk-mono" style={{ fontSize: 12 }}>{x.cron}</div><div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{describeCron(x.cron, t as any)}{status.data?.timeZone ? ` (${status.data.timeZone})` : ''}</div></div> : <span style={{ fontSize: 12 }}>{t('tasks.every', 'every')} <span className="hlk-mono">{x.interval}</span></span> },
            { title: t('common.enabled', 'Enabled'), sortValue: (x: any) => x.enabled, width: 80, render: (_: unknown, x) => <Switch size="small" checked={x.enabled} disabled={!canWrite} onChange={() => toggle.mutate(x)} /> },
            { title: t('tasks.lastRun', 'Last run'), sortValue: (x: any) => x.lastRun, width: 190, render: (_: unknown, x) => <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', fontSize: 12 }}><StatusDot status={x.running ? 'running' : x.lastStatus === 'failed' ? 'error' : x.lastStatus === 'success' ? 'success' : 'idle'} />{x.running ? t('tasks.runningNow', 'running…') : <><RelTime value={x.lastRun} empty={t('common.never', 'never')} />{x.lastStatus && <span style={{ color: x.lastStatus === 'failed' ? 'var(--hlk-error)' : 'var(--hlk-text-tertiary)' }}>· {x.lastStatus}</span>}</>}</span> },
            { title: t('tasks.nextRun', 'Next run'), sortValue: (x: any) => (x.enabled ? x.nextRun : null), width: 130, render: (_: unknown, x) => <span style={{ fontSize: 12 }}>{x.enabled ? <RelTime value={x.nextRun} /> : '—'}</span> },
            { title: '', width: 150, render: (_: unknown, x) => canWrite && <span style={{ fontSize: 12, display: 'flex', gap: 12 }}><a style={{ fontWeight: 500 }} onClick={() => run.mutate(x.name)}>{t('tasks.runNow', 'Run now')}</a><a onClick={() => { setSched(x); setMode(x.cron ? 'cron' : 'default'); setCron(x.cron ?? ''); setEnabled(x.enabled) }}>{t('tasks.scheduleBtn', 'Schedule')}</a></span> },
          ]}
        />
      </div>
      <div className="hlk-card" style={{ padding: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '12px 16px' }}>
          <span className="hlk-section-label" style={{ whiteSpace: 'nowrap' }}>{t('tasks.runs', 'Run history')}</span>
          <div style={{ flex: 1 }} />
          {/* A select, not buttons: with a dozen tasks a button row runs off the page. */}
          <Select size="small" style={{ width: 240, maxWidth: '60%' }} showSearch allowClear value={filterTask} onChange={(v) => setFilterTask(v || undefined)} placeholder={t('common.all', 'All')} options={(tasks.data ?? []).map((x) => ({ value: x.name, label: x.name }))} />
        </div>
        <SortableTable<TaskRun>
          rowKey="id" loading={runs.isLoading} dataSource={runs.data ?? []} className="hlk-table" size="small" pagination={{ pageSize: 20, showSizeChanger: false }}
          expandable={{ expandedRowRender: (r) => <pre className="hlk-logbox" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{r.log || t('tasks.noLog', '(no output)')}</pre> }}
          columns={[
            { title: t('common.time', 'Time'), dataIndex: 'startedAt', width: 180, render: (x: string) => <AbsTime value={x} /> },
            { title: t('tasks.task', 'Task'), dataIndex: 'taskName', width: 160, render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
            { title: t('common.status', 'Status'), dataIndex: 'status', width: 110, render: (x: string) => <Tag color={x === 'success' ? 'success' : x === 'failed' ? 'error' : 'processing'}>{x}</Tag> },
            { title: t('tasks.duration', 'Duration'), sortValue: (r: any) => (r.finishedAt ? Date.parse(r.finishedAt) - Date.parse(r.startedAt) : null), width: 90, render: (_: unknown, r) => <span className="hlk-num" style={{ fontSize: 12 }}>{dur(r)}</span> },
            { title: t('tasks.summary', 'Summary'), sortValue: (r: any) => r.log, render: (_: unknown, r) => <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 600 }}>{(r.log || '').split('\n').filter(Boolean).slice(-1)[0]}</span> },
          ]}
        />
      </div>
      <Modal open={!!sched} onCancel={() => setSched(null)} width={520} title={<span>{t('tasks.scheduleTitle', 'Schedule')} <span className="hlk-mono">{sched?.name}</span></span>} okText={t('tasks.saveSchedule', 'Save schedule')} onOk={() => save.mutate()} okButtonProps={{ loading: save.isPending, disabled: mode === 'cron' && !preview }}>
        <Segmented block value={mode} onChange={(v) => setMode(v as any)} options={[{ value: 'default', label: t('tasks.defaultInterval', 'Default interval ({{i}})', { i: sched?.interval }) }, { value: 'cron', label: 'Cron' }]} style={{ margin: '12px 0 16px' }} />
        {mode === 'cron' && (
          <>
            <Input className="hlk-mono" size="large" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 2 * * *" style={{ letterSpacing: '.06em' }} />
            <div style={{ fontSize: 12, color: cron && cronPreview.data && !preview ? 'var(--hlk-error)' : 'var(--hlk-text-secondary)', margin: '6px 0 10px' }}>
              {cron ? describeCron(cron, t as any) : t('cron.hint', 'minute hour day-of-month month day-of-week')}
              {serverZone && <> · {t('cron.serverZone', 'server time: {{zone}}', { zone: serverZone })}</>}
            </div>
            {serverZone && serverOffset !== localOffset && (
              <Alert type="info" showIcon style={{ marginBottom: 10 }} message={t('cron.zoneNote', 'Times in the expression are in the server\'s zone, {{zone}}, not yours. The runs below are shown in your time.', { zone: serverZone })} />
            )}
            <div style={{ display: 'flex', gap: 6, marginBottom: 12 }}>{templates.map((x) => <Tag.CheckableTag key={x.v} checked={cron === x.v} onChange={() => setCron(x.v)} style={{ border: '1px solid var(--hlk-border)' }}>{x.l}</Tag.CheckableTag>)}</div>
            {preview && (
              <div style={{ background: 'var(--hlk-bg)', borderRadius: 8, padding: '10px 12px', fontSize: 12 }}>
                <div className="hlk-section-label" style={{ marginBottom: 6 }}>{t('cron.next5', 'Next 5 runs')}</div>
                {preview.map((d, i) => (
                  <div key={i} className="hlk-mono">
                    {dayjs(d).format('YYYY-MM-DD HH:mm')}
                    {serverOffset !== localOffset && <span style={{ color: 'var(--hlk-text-tertiary)' }}> · {t('cron.serverAt', 'server {{time}}', { time: serverTime(d) })}</span>}
                  </div>
                ))}
              </div>
            )}
          </>
        )}
        <div style={{ marginTop: 16 }}><Switch checked={enabled} onChange={setEnabled} /> <span style={{ marginLeft: 8 }}>{t('common.enabled', 'Enabled')}</span></div>
        {sched?.name === 'cleanup-policies' && <Alert type="info" showIcon style={{ marginTop: 12 }} message={t('tasks.cleanupNote', 'Cleanup policies only take effect when this task runs.')} />}
      </Modal>
    </>
  )
}
