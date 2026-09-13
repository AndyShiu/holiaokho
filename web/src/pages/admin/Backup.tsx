import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, App, Button, Checkbox, Input, InputNumber, Modal, Skeleton, Switch, Upload } from 'antd'
import { CloudUploadOutlined, DownloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, get, put } from '@/api/client'
import type { BackupSettings, Health, Task, TaskRun } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, useErrorText } from '@/components/Common'
import { fmtBytes, RelTime, StatusDot } from '@/components/Format'

export default function Backup() {
  const { t } = useTranslation()
  const { logout, can } = useAuth()
  const canAdmin = can('app:system', 'admin')
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [blobs, setBlobs] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [step, setStep] = useState<0 | 1 | 2>(0)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [log, setLog] = useState<string[] | null>(null)
  const settings = useQuery({ queryKey: ['backup-settings'], queryFn: () => get<BackupSettings>('system/backup-settings') })
  const [draft, setDraft] = useState<BackupSettings | null>(null)
  const [dirty, setDirty] = useState(false)
  useEffect(() => { if (settings.data && !draft) setDraft(settings.data) }, [settings.data, draft])
  const edit = (patch: Partial<BackupSettings>) => { setDraft((d) => (d ? { ...d, ...patch } : d)); setDirty(true) }
  const save = useMutation({
    mutationFn: (v: BackupSettings) => put<BackupSettings>('system/backup-settings', v),
    onSuccess: (v) => { setDraft(v); setDirty(false); message.success(t('common.saved', 'Saved')); qc.invalidateQueries({ queryKey: ['tasks'] }) },
    onError: (e) => message.error(errText(e)),
  })
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks') })
  const runs = useQuery({ queryKey: ['task-runs', 'backup'], queryFn: () => get<TaskRun[]>('tasks/runs', { task: 'backup' }) })
  const health = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check') })
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
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
            <div className="hlk-section-label">{t('backup.scheduled', 'Scheduled backup')}</div>
            <div style={{ flex: 1 }} />
            {dirty && <span className="hlk-unsaved">● {t('common.unsaved', 'Unsaved changes')}</span>}
            <Button type="primary" size="small" disabled={!dirty || !canAdmin} loading={save.isPending} onClick={() => draft && save.mutate(draft)}>
              {t('common.save', 'Save')}
            </Button>
          </div>
          {draft ? (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
                <Switch checked={draft.enabled} disabled={!canAdmin} onChange={(v) => edit({ enabled: v })} />
                <span style={{ fontSize: 13 }}>{draft.enabled ? t('common.enabled', 'Enabled') : t('common.off', 'off')}</span>
              </div>
              <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 4 }}>{t('backup.dir', 'Directory')}</div>
              <Input className="hlk-mono" value={draft.dir} disabled={!canAdmin} onChange={(e) => edit({ dir: e.target.value })} placeholder="/data/backups" />
              <div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)', margin: '4px 0 12px' }}>{t('backup.dirHint', 'Absolute path on the server (inside the container when deployed with Docker/K8s).')}</div>
              <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                <div>
                  <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 4 }}>{t('backup.keep', 'Keep')}</div>
                  <InputNumber min={1} max={365} style={{ width: '100%' }} value={draft.keep} disabled={!canAdmin} onChange={(v) => edit({ keep: v ?? 7 })} />
                </div>
                <div>
                  <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 4 }}>Cron</div>
                  <Input className="hlk-mono" value={draft.cron} disabled={!canAdmin} onChange={(e) => edit({ cron: e.target.value })} placeholder="0 2 * * *" />
                </div>
              </div>
              <div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)', margin: '4px 0 12px' }}>{t('backup.cronHint', 'Leave empty to run once a day. Set the schedule precisely under Tasks.')}</div>
              <Checkbox checked={draft.withBlobs} disabled={!canAdmin} onChange={(e) => edit({ withBlobs: e.target.checked })}>
                {t('backup.includeBlobs', 'Include blobs')} <span style={{ color: 'var(--hlk-text-tertiary)' }}>(≈ {fmtBytes(total)})</span>
              </Checkbox>
              <div style={{ marginTop: 14, fontSize: 12, color: 'var(--hlk-text-secondary)', display: 'flex', gap: 8, alignItems: 'center' }}>
                {t('tasks.lastRun', 'Last run')}:
                {backupTask ? <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><StatusDot status={backupTask.lastStatus === 'failed' ? 'error' : backupTask.lastStatus === 'success' ? 'success' : 'idle'} /><RelTime value={backupTask.lastRun} empty={t('common.never', 'never')} /></span> : '—'}
                <Link to="/admin/tasks">{t('nav.tasks', 'Tasks')} →</Link>
              </div>
              {runs.data && runs.data.length > 0 && <div style={{ marginTop: 10, fontSize: 12 }}>{runs.data.slice(0, 5).map((r) => <div key={r.id} style={{ display: 'flex', gap: 8, padding: '3px 0' }}><StatusDot status={r.status === 'failed' ? 'error' : 'success'} /><RelTime value={r.startedAt} /><span style={{ color: 'var(--hlk-text-tertiary)' }}>{r.status}</span></div>)}</div>}
            </>
          ) : <Skeleton active />}
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
