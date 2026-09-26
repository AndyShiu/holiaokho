import { useState } from 'react'
import { Alert, App, Button, Drawer, Form, Input, Select, Switch, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, post, put } from '@/api/client'
import type { Repository, Webhook } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, SecretHint, useErrorText } from '@/components/Common'
import { CodeBlock } from '@/components/Copyable'
import { RelTime } from '@/components/Format'
import { SortableTable } from '@/components/SortableTable'

const EVENTS = ['asset.created', 'asset.deleted', 'package.deleted', 'repository.create', 'repository.update', 'repository.delete', 'repository.invalidate_cache', 'user.create', 'user.update', 'user.delete', 'task.failed', '*']

export default function Webhooks() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [drawer, setDrawer] = useState<'new' | Webhook | null>(null)
  const [toDelete, setToDelete] = useState<Webhook | null>(null)
  const [form] = Form.useForm()
  const hooks = useQuery({ queryKey: ['webhooks'], queryFn: () => get<Webhook[]>('webhooks') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const canWrite = can('app:system', 'write')
  const editing = drawer && drawer !== 'new' ? drawer : null
  const save = useMutation({
    mutationFn: (v: any) => { if (v.secret === '***' || v.secret === '') delete v.secret; return drawer === 'new' ? post('webhooks', v) : put(`webhooks/${editing!.id}`, v) },
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); qc.invalidateQueries({ queryKey: ['webhooks'] }) },
  })
  const toggle = useMutation({ mutationFn: (h: Webhook) => put(`webhooks/${h.id}`, { ...h, secret: undefined, enabled: !h.enabled }), onSuccess: () => qc.invalidateQueries({ queryKey: ['webhooks'] }), onError: (e) => message.error(errText(e)) })
  const remove = useMutation({ mutationFn: (id: string) => del(`webhooks/${id}`), onSuccess: () => { setToDelete(null); qc.invalidateQueries({ queryKey: ['webhooks'] }) }, onError: (e) => message.error(errText(e)) })
  const test = async (h: Webhook) => { try { const r = await post<{ queued: boolean; message?: string }>(`webhooks/${h.id}/test`); message.success(r.message ?? t('webhooks.queued', 'Test event queued — check Logs for the delivery result')) } catch (e) { message.error(errText(e)) } }
  const payload = `{
  "event": "asset.created",
  "at": "2026-09-13T12:00:00Z",
  "repository": "maven-releases",
  "data": { "path": "com/acme/lib/1.0/lib-1.0.jar", "size": 12345, "sha256": "…" }
}`
  const verify = `// Node.js
const sig = req.headers['x-holiaokho-signature']            // "sha256=<hex>"
const mac = crypto.createHmac('sha256', SECRET).update(rawBody).digest('hex')
const ok = crypto.timingSafeEqual(Buffer.from(sig.slice(7)), Buffer.from(mac))`

  return (
    <>
      <PageHeader title={t('nav.webhooks', 'Webhooks')} count={hooks.data?.length} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setDrawer('new') }}>{t('webhooks.create', 'Create webhook')}</Button>} />
      <div className="hlk-card" style={{ padding: 0, marginBottom: 16 }}>
        <SortableTable<Webhook>
          rowKey="id" loading={hooks.isLoading} dataSource={hooks.data ?? []} className="hlk-table hlk-clickable" scroll={{ x: 1050 }} pagination={false} size="middle"
          onRow={(h) => ({ onClick: () => { form.setFieldsValue({ ...h, secret: h.secret ? '***' : '' }); setDrawer(h) } })}
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', width: 180, render: (x: string) => <span style={{ fontWeight: 500 }}>{x}</span> },
            { title: 'URL', dataIndex: 'url', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
            { title: t('webhooks.events', 'Events'), dataIndex: 'events', render: (ev: string[]) => ev.map((e) => <Tag key={e} className="hlk-mono" style={{ fontSize: 11 }}>{e}</Tag>) },
            { title: t('common.repository', 'Repository'), dataIndex: 'repository', width: 140, render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x || t('common.all', 'All')}</span> },
            { title: t('common.enabled', 'Enabled'), dataIndex: 'enabled', width: 80, render: (e: boolean, h) => <span onClick={(ev) => ev.stopPropagation()}><Switch size="small" checked={e} disabled={!canWrite} onChange={() => toggle.mutate(h)} /></span> },
            { title: t('common.created', 'Created'), dataIndex: 'createdAt', width: 120, render: (x: string) => <RelTime value={x} /> },
            { title: '', width: 160, render: (_: unknown, h) => canWrite && <span style={{ fontSize: 12, display: 'flex', gap: 10 }} onClick={(e) => e.stopPropagation()}><a onClick={() => test(h)}>{t('webhooks.sendTest', 'Send test')}</a>{can('app:system', 'delete') && <a style={{ color: 'var(--hlk-error)' }} onClick={() => setToDelete(h)}>{t('common.delete', 'Delete')}</a>}</span> },
          ]}
        />
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(380px, 1fr))', gap: 16 }}>
        <CodeBlock title={t('webhooks.payload', 'Payload example')} code={payload} />
        <CodeBlock title={t('webhooks.signature', 'Signature: X-Holiaokho-Signature (HMAC-SHA256 of the body)')} code={verify} />
      </div>
      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={600} title={drawer === 'new' ? t('webhooks.create', 'Create webhook') : t('webhooks.edit', 'Edit webhook')} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <Form form={form} layout="vertical" initialValues={{ enabled: true, events: ['asset.created'] }} onFinish={(v) => save.mutate(v)} disabled={!canWrite}>
          <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="url" label="URL" rules={[{ required: true, type: 'url' }]} extra={t('webhooks.httpsHint', 'https:// recommended')}><Input className="hlk-mono" placeholder="https://hooks.example.com/holiaokho" /></Form.Item>
          <Form.Item name="events" label={t('webhooks.events', 'Events')} rules={[{ required: true }]}><Select mode="multiple" options={EVENTS.map((e) => ({ value: e }))} className="hlk-mono" /></Form.Item>
          <Form.Item name="repository" label={t('common.repository', 'Repository')} extra={t('webhooks.repoHint', 'Empty = all repositories')}><Select allowClear showSearch options={(repos.data ?? []).map((r) => ({ value: r.name }))} /></Form.Item>
          <Form.Item name="secret" label={t('webhooks.secret', 'Secret')} extra={editing && <SecretHint />}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="enabled" label={t('common.enabled', 'Enabled')} valuePropName="checked"><Switch /></Form.Item>
          <div style={{ display: 'flex', gap: 8 }}><Button type="primary" htmlType="submit" loading={save.isPending}>{t('common.save', 'Save')}</Button><Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button></div>
        </Form>
      </Drawer>
      <ConfirmDelete open={!!toDelete} name={toDelete?.name ?? ''} typeToConfirm={false} title={t('webhooks.deleteTitle', 'Delete webhook "{{name}}"?', { name: toDelete?.name })} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.id) }} loading={remove.isPending} />
    </>
  )
}
