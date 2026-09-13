import { useState } from 'react'
import { Alert, App, Button, Drawer, Form, Input, Table } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, post, put } from '@/api/client'
import type { ContentSelector, Role } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, useErrorText } from '@/components/Common'

export function TestPanel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ background: 'var(--hlk-card-alt)', border: '1px solid var(--hlk-border)', borderRadius: 10, padding: 16 }}>
      <div className="hlk-section-label" style={{ marginBottom: 12 }}>{title}</div>
      {children}
    </div>
  )
}

export default function Selectors() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [drawer, setDrawer] = useState<'new' | ContentSelector | null>(null)
  const [toDelete, setToDelete] = useState<ContentSelector | null>(null)
  const [test, setTest] = useState({ format: 'maven', path: '/com/acme/lib/1.0/lib-1.0.jar' })
  const [result, setResult] = useState<{ matches?: boolean; error?: string } | null>(null)
  const [form] = Form.useForm()
  const list = useQuery({ queryKey: ['content-selectors'], queryFn: () => get<ContentSelector[]>('content-selectors') })
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => get<Role[]>('roles') })
  const canWrite = can('app:roles', 'write')
  const editing = drawer && drawer !== 'new' ? drawer : null
  const usedBy = (name: string) => (roles.data ?? []).filter((r) => r.privileges.some((p) => p.target.startsWith(`selector:${name}@`))).map((r) => r.id)
  const save = useMutation({
    mutationFn: (v: any) => (drawer === 'new' ? post('content-selectors', v) : put(`content-selectors/${editing!.id}`, v)),
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); qc.invalidateQueries({ queryKey: ['content-selectors'] }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => del(`content-selectors/${id}`), onSuccess: () => { setToDelete(null); qc.invalidateQueries({ queryKey: ['content-selectors'] }) }, onError: (e) => message.error(errText(e)) })
  const runTest = async () => {
    try {
      const r = await post<{ matches: boolean }>('content-selectors/test', { expression: form.getFieldValue('expression'), format: test.format, path: test.path })
      setResult({ matches: r.matches })
    } catch (e) {
      setResult({ error: errText(e) })
    }
  }
  return (
    <>
      <PageHeader title={t('nav.selectors', 'Content Selectors')} count={list.data?.length} sub={t('selectors.sub', 'Expressions that pick a subset of a repository, usable as privilege targets (selector:<name>@<repo>).')} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setResult(null); setDrawer('new') }}>{t('selectors.create', 'Create selector')}</Button>} />
      <div className="hlk-card" style={{ padding: 0 }}>
        <Table<ContentSelector>
          rowKey="id" loading={list.isLoading} dataSource={list.data ?? []} className="hlk-table hlk-clickable" pagination={false} size="middle"
          onRow={(s) => ({ onClick: () => { form.setFieldsValue(s); setResult(null); setDrawer(s) } })}
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', width: 200, render: (x: string) => <span className="hlk-mono" style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('selectors.expression', 'Expression'), dataIndex: 'expression', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
            { title: t('common.description', 'Description'), dataIndex: 'description', render: (x: string) => <span style={{ color: 'var(--hlk-text-secondary)' }}>{x}</span> },
            { title: t('selectors.usedBy', 'Used by roles'), width: 200, render: (_: unknown, s) => <span style={{ fontSize: 12 }}>{usedBy(s.name).join(', ') || '—'}</span> },
            { title: '', width: 80, render: (_: unknown, s) => can('app:roles', 'delete') && <a style={{ color: 'var(--hlk-error)', fontSize: 12 }} onClick={(e) => { e.stopPropagation(); setToDelete(s) }}>{t('common.delete', 'Delete')}</a> },
          ]}
        />
      </div>
      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={820} title={drawer === 'new' ? t('selectors.create', 'Create selector') : t('selectors.edit', 'Edit selector')} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 300px', gap: 24 }}>
          <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} disabled={!canWrite}>
            <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }, { pattern: /^[A-Za-z0-9._-]+$/, message: t('repos.nameInvalid', 'Only letters, digits, . _ -') }]}><Input className="hlk-mono" disabled={!!editing} /></Form.Item>
            <Form.Item name="description" label={t('common.description', 'Description')}><Input /></Form.Item>
            <Form.Item name="expression" label={t('selectors.expression', 'Expression')} rules={[{ required: true }]}><Input.TextArea rows={3} className="hlk-mono" placeholder='format == "maven" and path =^ "/com/acme/"' /></Form.Item>
            <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', lineHeight: 1.8, marginBottom: 16 }}>
              <div><code>format == "maven"</code> · <code>path =^ "/prefix/"</code> ({t('selectors.prefix', 'starts with')}) · <code>path =~ "regex"</code></div>
              <div><code>and</code> / <code>or</code> / <code>not</code> / <code>( )</code></div>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <Button type="primary" htmlType="submit" loading={save.isPending}>{t('common.save', 'Save')}</Button>
              <Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button>
            </div>
          </Form>
          <TestPanel title={t('common.test', 'Test')}>
            <div style={{ fontSize: 12, marginBottom: 4 }}>{t('common.format', 'Format')}</div>
            <Input size="small" className="hlk-mono" value={test.format} onChange={(e) => setTest({ ...test, format: e.target.value })} style={{ marginBottom: 10 }} />
            <div style={{ fontSize: 12, marginBottom: 4 }}>{t('asset.path', 'Path')}</div>
            <Input size="small" className="hlk-mono" value={test.path} onChange={(e) => setTest({ ...test, path: e.target.value })} style={{ marginBottom: 12 }} />
            <Button size="small" onClick={runTest}>{t('selectors.runTest', 'Evaluate')}</Button>
            {result && (
              <div style={{ marginTop: 12, fontSize: 13, color: result.error ? 'var(--hlk-error)' : result.matches ? 'var(--hlk-success)' : 'var(--hlk-text-secondary)' }}>
                {result.error ? result.error : result.matches ? '✓ ' + t('selectors.match', 'Matches') : '✗ ' + t('selectors.noMatch', 'Does not match')}
              </div>
            )}
          </TestPanel>
        </div>
      </Drawer>
      <ConfirmDelete open={!!toDelete} name={toDelete?.name ?? ''} typeToConfirm={false} title={<span>{t('selectors.deleteTitle', 'Delete selector')} <span className="hlk-mono">{toDelete?.name}</span>?</span>} description={toDelete && usedBy(toDelete.name).length ? t('selectors.deleteUsed', 'Roles referencing it: {{list}}. Their selector privileges stop matching.', { list: usedBy(toDelete.name).join(', ') }) : undefined} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.id) }} loading={remove.isPending} />
    </>
  )
}
