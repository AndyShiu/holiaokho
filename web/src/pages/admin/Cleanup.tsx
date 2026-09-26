import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, App, Button, Drawer, Form, Input, InputNumber, Radio, Select, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, post, put } from '@/api/client'
import type { CleanupPolicy, Package, Repository, Status, Task } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, useErrorText } from '@/components/Common'
import { RelTime } from '@/components/Format'
import { formatInfo } from '@/theme/tokens'
import { TestPanel } from './Selectors'
import { SortableTable } from '@/components/SortableTable'

function summary(p: CleanupPolicy, t: (k: string, d: string, o?: any) => string) {
  const c = p.criteria
  const parts: string[] = []
  if (c.lastDownloadedDays) parts.push(t('cleanup.sumDl', 'not downloaded in {{n}} days', { n: c.lastDownloadedDays }))
  if (c.lastUpdatedDays) parts.push(t('cleanup.sumUpd', 'older than {{n}} days', { n: c.lastUpdatedDays }))
  if (c.keepLatest) parts.push(t('cleanup.sumKeep', 'keep newest {{n}} per name', { n: c.keepLatest }))
  if (c.nameRegex) parts.push(`name ~ ${c.nameRegex}`)
  if (c.versionRegex) parts.push(`version ~ ${c.versionRegex}`)
  if (c.prerelease === 'true') parts.push(t('cleanup.sumPre', 'prereleases only'))
  if (c.prerelease === 'false') parts.push(t('cleanup.sumRel', 'releases only'))
  return parts.join(' · ')
}

export default function Cleanup() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [drawer, setDrawer] = useState<'new' | CleanupPolicy | null>(null)
  const [toDelete, setToDelete] = useState<CleanupPolicy | null>(null)
  const [previewRepo, setPreviewRepo] = useState<string | undefined>()
  const [preview, setPreview] = useState<{ wouldDelete: number; packages: Package[] } | null>(null)
  const [previewBusy, setPreviewBusy] = useState(false)
  const [assigned, setAssigned] = useState<string[]>([])
  const [form] = Form.useForm()
  const policies = useQuery({ queryKey: ['cleanup-policies'], queryFn: () => get<CleanupPolicy[]>('cleanup-policies') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status') })
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks'), enabled: can('app:tasks', 'read') })
  const canWrite = can('app:repositories', 'write')
  const editing = drawer && drawer !== 'new' ? drawer : null
  const fmt = Form.useWatch('format', form)
  const cleanupTask = tasks.data?.find((x) => x.name === 'cleanup-policies')
  useEffect(() => { setPreview(null) }, [drawer])

  const save = useMutation({
    mutationFn: async (v: any) => {
      const body = { name: v.name, format: v.format === '*' ? '' : v.format, criteria: { lastDownloadedDays: v.lastDownloadedDays ?? 0, lastUpdatedDays: v.lastUpdatedDays ?? 0, keepLatest: v.keepLatest ?? 0, nameRegex: v.nameRegex ?? '', versionRegex: v.versionRegex ?? '', prerelease: v.prerelease ?? '' } }
      const saved = drawer === 'new' ? await post<CleanupPolicy>('cleanup-policies', body) : await put<CleanupPolicy>(`cleanup-policies/${editing!.id}`, body)
      const id = saved?.id ?? editing!.id
      const cur = editing?.repositories ?? []
      for (const r of assigned) if (!cur.includes(r)) await put(`cleanup-policies/${id}/repositories/${r}`)
      for (const r of cur) if (!assigned.includes(r)) await del(`cleanup-policies/${id}/repositories/${r}`)
    },
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); qc.invalidateQueries({ queryKey: ['cleanup-policies'] }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => del(`cleanup-policies/${id}`), onSuccess: () => { setToDelete(null); qc.invalidateQueries({ queryKey: ['cleanup-policies'] }) }, onError: (e) => message.error(errText(e)) })
  const runPreview = async () => {
    if (!editing || !previewRepo) return
    setPreviewBusy(true)
    try {
      setPreview(await post(`cleanup-policies/${editing.id}/preview`, undefined, { repository: previewRepo, limit: 200 }))
    } catch (e) {
      message.error(errText(e))
    } finally {
      setPreviewBusy(false)
    }
  }
  const open = (p: CleanupPolicy | 'new') => {
    if (p === 'new') { form.resetFields(); setAssigned([]) } else { form.setFieldsValue({ name: p.name, format: p.format || '*', ...p.criteria, prerelease: p.criteria.prerelease ?? '' }); setAssigned(p.repositories ?? []) }
    setPreviewRepo(undefined)
    setDrawer(p)
  }
  const repoOptions = (repos.data ?? []).filter((r) => r.type !== 'group' && (!fmt || fmt === '*' || r.format === fmt)).map((r) => ({ value: r.name, label: `${r.name} (${r.format})` }))
  const anyCriteria = (v: any) => !!(v.lastDownloadedDays || v.lastUpdatedDays || v.keepLatest || v.nameRegex || v.versionRegex || v.prerelease)

  return (
    <>
      <PageHeader title={t('nav.cleanup', 'Cleanup Policies')} count={policies.data?.length} sub={cleanupTask && <span>{t('cleanup.nextRun', 'Applied by the cleanup-policies task · next run')} <RelTime value={cleanupTask.nextRun} /> · <Link to="/admin/tasks">{t('nav.tasks', 'Tasks')}</Link></span>} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => open('new')}>{t('cleanup.create', 'Create policy')}</Button>} />
      <div className="hlk-card" style={{ padding: 0 }}>
        <SortableTable<CleanupPolicy>
          rowKey="id" loading={policies.isLoading} dataSource={policies.data ?? []} className="hlk-table hlk-clickable" pagination={false} size="middle"
          onRow={(p) => ({ onClick: () => open(p) })}
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', width: 200, render: (x: string) => <span style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('common.format', 'Format'), dataIndex: 'format', width: 120, render: (x: string) => (x ? formatInfo(x).label : <Tag>{t('common.all', 'All')}</Tag>) },
            { title: t('cleanup.criteria', 'Criteria'), sortValue: (p: any) => summary(p, t as any), render: (_: unknown, p) => <span style={{ fontSize: 12 }}>{summary(p, t as any) || '—'}</span> },
            { title: t('cleanup.assigned', 'Repositories'), sortValue: (p: any) => (p.repositories ?? []).length, width: 260, render: (_: unknown, p) => (p.repositories ?? []).map((r) => <Tag key={r} className="hlk-mono" style={{ fontSize: 11 }}>{r}</Tag>) },
            { title: '', width: 80, render: (_: unknown, p) => can('app:repositories', 'delete') && <a style={{ color: 'var(--hlk-error)', fontSize: 12 }} onClick={(e) => { e.stopPropagation(); setToDelete(p) }}>{t('common.delete', 'Delete')}</a> },
          ]}
        />
      </div>
      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={860} title={drawer === 'new' ? t('cleanup.create', 'Create policy') : t('cleanup.edit', 'Edit policy')} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <div className="hlk-side-grid">
          <Form form={form} layout="vertical" initialValues={{ format: '*', prerelease: '' }} onFinish={(v) => save.mutate(v)} disabled={!canWrite}>
            <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }]}><Input /></Form.Item>
              <Form.Item name="format" label={t('common.format', 'Format')}><Select options={[{ value: '*', label: t('common.all', 'All formats') }, ...(status.data?.formats ?? []).map((f) => ({ value: f, label: formatInfo(f).label }))]} /></Form.Item>
            </div>
            <div className="hlk-section-label" style={{ marginBottom: 8 }}>{t('cleanup.criteria', 'Criteria')} <span style={{ textTransform: 'none', letterSpacing: 0 }}>· {t('cleanup.criteriaHint', 'a package is removed when ANY age criterion matches; keepLatest always protects the newest versions')}</span></div>
            <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
              <Form.Item name="lastDownloadedDays" label={t('cleanup.lastDl', 'Not downloaded for (days)')}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
              <Form.Item name="lastUpdatedDays" label={t('cleanup.lastUpd', 'Older than (days)')}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
              <Form.Item name="keepLatest" label={t('cleanup.keep', 'Keep newest N per name')}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
            </div>
            <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Form.Item name="nameRegex" label={t('cleanup.nameRegex', 'Name regex')}><Input className="hlk-mono" placeholder="^com\\.acme\\." /></Form.Item>
              <Form.Item name="versionRegex" label={t('cleanup.versionRegex', 'Version regex')}><Input className="hlk-mono" placeholder="-SNAPSHOT$" /></Form.Item>
            </div>
            <Form.Item name="prerelease" label={t('cleanup.prerelease', 'Prerelease')}>
              <Radio.Group options={[{ value: '', label: t('cleanup.preAny', 'Any') }, { value: 'true', label: t('cleanup.preOnly', 'Only prereleases') }, { value: 'false', label: t('cleanup.preExclude', 'Only releases') }]} />
            </Form.Item>
            <Form.Item label={t('cleanup.assignTo', 'Apply to repositories')}><Select mode="multiple" value={assigned} onChange={setAssigned} options={repoOptions} placeholder={t('common.none', 'None')} /></Form.Item>
            <Form.Item noStyle shouldUpdate>{() => (
              <div style={{ display: 'flex', gap: 8 }}>
                <Button type="primary" htmlType="submit" loading={save.isPending} disabled={!anyCriteria(form.getFieldsValue())}>{t('common.save', 'Save')}</Button>
                <Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button>
              </div>
            )}</Form.Item>
          </Form>
          <TestPanel title={t('cleanup.preview', 'Preview')}>
            {!editing ? <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{t('cleanup.previewSaveFirst', 'Save the policy first, then preview what it would delete.')}</div> : (
              <>
                <Select size="small" style={{ width: '100%', marginBottom: 8 }} placeholder={t('common.repository', 'Repository')} value={previewRepo} onChange={setPreviewRepo} options={repoOptions} />
                <Button size="small" onClick={runPreview} loading={previewBusy} disabled={!previewRepo}>{t('cleanup.runPreview', 'Preview deletions')}</Button>
                {preview && (
                  <div style={{ marginTop: 12 }}>
                    <Alert type={preview.wouldDelete > 100 ? 'warning' : 'info'} showIcon message={t('cleanup.wouldDelete', 'Would delete {{n}} packages', { n: preview.wouldDelete })} />
                    <div style={{ maxHeight: 320, overflow: 'auto', marginTop: 8, fontSize: 11 }} className="hlk-mono">
                      {preview.packages.map((p) => <div key={p.id} style={{ padding: '3px 0', borderBottom: '1px solid var(--hlk-row)' }}>{p.namespace ? p.namespace + '/' : ''}{p.name}@{p.version}</div>)}
                      {preview.wouldDelete > preview.packages.length && <div style={{ color: 'var(--hlk-text-tertiary)', padding: 4 }}>… +{preview.wouldDelete - preview.packages.length}</div>}
                    </div>
                  </div>
                )}
              </>
            )}
          </TestPanel>
        </div>
      </Drawer>
      <ConfirmDelete open={!!toDelete} name={toDelete?.name ?? ''} typeToConfirm={false} title={t('cleanup.deleteTitle', 'Delete policy "{{name}}"?', { name: toDelete?.name })} description={t('cleanup.deleteHint', 'It is unassigned from {{n}} repositories. Nothing already deleted is restored.', { n: toDelete?.repositories?.length ?? 0 })} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.id) }} loading={remove.isPending} />
    </>
  )
}
