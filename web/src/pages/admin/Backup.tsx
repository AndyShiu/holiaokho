import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, App, Button, Checkbox, Input, Modal, Upload } from 'antd'
import { CloudUploadOutlined, DownloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, get } from '@/api/client'
import type { Health, Task, TaskRun } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { KV, PageHeader, useErrorText } from '@/components/Common'
import { fmtBytes, RelTime, StatusDot } from '@/components/Format'

export default function Backup() {
  const { t } = useTranslation()
  const { logout } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [blobs, setBlobs] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [step, setStep] = useState<0 | 1 | 2>(0)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [log, setLog] = useState<string[] | null>(null)
  const cfg = useQuery({ queryKey: ['system-config'], queryFn: () => get<any>('system/config') })
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks') })
  const runs = useQuery({ queryKey: ['task-runs', 'backup'], queryFn: () => get<TaskRun[]>('tasks/runs', { task: 'backup' }) })
  const health = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check') })
  const b = cfg.data?.backup ?? cfg.data?.Backup ?? {}
  const backupTask = tasks.data?.find((x) => x.name === 'backup')
  const total = Object.values(health.data?.checks ?? {}).reduce((a, c) => a + (c.usedBytes ?? 0), 0)

  const restore = async () => {
    if (!file) return
    setBusy(true)
    try {
      const res = await api<Response>('system/restore', { method: 'POST', body: file, headers: { 'Content-Type': 'application/gzip' }, raw: true })
      const data = await res.json()
      if (!res.ok) throw new Error(data?.message ?? res.statusText)
      setLog(data.log ?? [])
      setStep(0)
      message.success(t('backup.restored', 'Restore complete — please log in again'))
      setTimeout(() => logout(), 1500)
    } catch (e) {
      message.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader title={t('nav.backup', 'Backup / Restore')} />
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16, marginBottom: 16 }}>
        <div className="hlk-card">
          <div className="hlk-section-label" style={{ marginBottom: 12 }}>{t('backup.scheduled', 'Scheduled backup')}</div>
          <KV items={[
            [t('backup.dir', 'Directory'), <span className="hlk-mono">{b.dir ?? b.Dir ?? '—'}</span>],
            [t('backup.includeBlobs', 'Include blobs'), String(b.include_blobs ?? b.IncludeBlobs ?? false)],
            [t('backup.keep', 'Keep'), String(b.keep ?? b.Keep ?? '—')],
            ['Cron', <span className="hlk-mono">{backupTask?.cron ?? b.cron ?? b.Cron ?? '—'}</span>],
            [t('tasks.lastRun', 'Last run'), backupTask ? <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><StatusDot status={backupTask.lastStatus === 'failed' ? 'error' : backupTask.lastStatus === 'success' ? 'success' : 'idle'} /><RelTime value={backupTask.lastRun} empty={t('common.never', 'never')} /></span> : t('backup.notConfigured', 'not configured')],
          ]} />
          <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginTop: 12 }}>{t('backup.cfgHint', 'Configured in the config file (backup.*). Runs as the "backup" task.')} <Link to="/admin/tasks">{t('nav.tasks', 'Tasks')}</Link></div>
          {runs.data && runs.data.length > 0 && <div style={{ marginTop: 12, fontSize: 12 }}>{runs.data.slice(0, 5).map((r) => <div key={r.id} style={{ display: 'flex', gap: 8, padding: '3px 0' }}><StatusDot status={r.status === 'failed' ? 'error' : 'success'} /><RelTime value={r.startedAt} /><span style={{ color: 'var(--hlk-text-tertiary)' }}>{r.status}</span></div>)}</div>}
        </div>
        <div className="hlk-card">
          <div className="hlk-section-label" style={{ marginBottom: 12 }}>{t('backup.download', 'Download a backup now')}</div>
          <p style={{ fontSize: 13, margin: '0 0 12px' }}>{t('backup.downloadHint', 'A tar.gz with the database (CSV per table) and, optionally, every blob.')}</p>
          <Checkbox checked={blobs} onChange={(e) => setBlobs(e.target.checked)}>{t('backup.includeBlobs', 'Include blobs')} <span style={{ color: 'var(--hlk-text-tertiary)' }}>(≈ {fmtBytes(total)})</span></Checkbox>
          <div style={{ marginTop: 16 }}><Button type="primary" icon={<DownloadOutlined />} href={`/api/v1/system/backup?blobs=${blobs}`}>{t('common.download', 'Download')}</Button></div>
        </div>
      </div>
      <div className="hlk-card" style={{ borderColor: 'var(--hlk-error)' }}>
        <div className="hlk-section-label" style={{ color: 'var(--hlk-error)', marginBottom: 12 }}>{t('backup.restore', 'Restore')}</div>
        <Alert type="error" showIcon style={{ marginBottom: 16 }} message={t('backup.restoreWarn', 'Restoring replaces the entire database and blob store with the archive contents. Stop client traffic first. Everyone is logged out afterwards.')} />
        <Upload.Dragger multiple={false} accept=".gz,.tgz,.tar.gz" beforeUpload={(f) => { setFile(f); return false }} showUploadList={false} style={{ maxWidth: 560 }}>
          <p><CloudUploadOutlined style={{ fontSize: 28, color: 'var(--hlk-text-tertiary)' }} /></p>
          <p>{file ? <span className="hlk-mono">{file.name} · {fmtBytes(file.size)}</span> : t('backup.drop', 'Drop a backup .tar.gz here')}</p>
        </Upload.Dragger>
        <Button danger style={{ marginTop: 16 }} disabled={!file} onClick={() => { setTyped(''); setStep(1) }}>{t('backup.restoreBtn', 'Restore from this file…')}</Button>
        {log && <pre className="hlk-logbox" style={{ marginTop: 16, maxHeight: 300 }}>{log.join('\n')}</pre>}
      </div>
      <Modal open={step === 1} onCancel={() => setStep(0)} title={t('backup.confirm1', 'Restore will overwrite everything')} okText={t('common.continue', 'Continue')} okButtonProps={{ danger: true }} onOk={() => setStep(2)}>
        <ul style={{ paddingLeft: 18, fontSize: 13, lineHeight: 1.8 }}>
          <li>{t('backup.c1', 'All repositories, users, roles, settings and content indexes are replaced.')}</li>
          <li>{t('backup.c2', 'Blobs in the archive are written into the default storage.')}</li>
          <li>{t('backup.c3', 'This can take several minutes; do not close the page.')}</li>
        </ul>
      </Modal>
      <Modal open={step === 2} onCancel={() => setStep(0)} title={t('backup.confirm2', 'Type RESTORE to confirm')} okText={t('backup.restoreNow', 'Restore now')} okButtonProps={{ danger: true, disabled: typed !== 'RESTORE', loading: busy }} onOk={restore} closable={!busy} maskClosable={false}>
        <Input className="hlk-mono" value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="RESTORE" autoFocus disabled={busy} />
        {busy && <div style={{ marginTop: 12, fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{t('backup.restoring', 'Restoring… this may take a while.')}</div>}
      </Modal>
    </>
  )
}
