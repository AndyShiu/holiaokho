import { useState } from 'react'
import { Alert, App, Button, Drawer, Form, Input, Radio, Tag, Tooltip } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, post, put } from '@/api/client'
import type { Repository, RoutingRule } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, useErrorText } from '@/components/Common'
import { TestPanel } from './Selectors'
import { SortableTable } from '@/components/SortableTable'

export default function RoutingRules() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [drawer, setDrawer] = useState<'new' | RoutingRule | null>(null)
  const [toDelete, setToDelete] = useState<RoutingRule | null>(null)
  const [matchers, setMatchers] = useState<string[]>([''])
  const [testPath, setTestPath] = useState('/com/acme/lib/1.0/lib-1.0.jar')
  const [result, setResult] = useState<{ allowed?: boolean; error?: string } | null>(null)
  const [form] = Form.useForm()
  const rules = useQuery({ queryKey: ['routing-rules'], queryFn: () => get<RoutingRule[]>('routing-rules') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const canWrite = can('app:repositories', 'write')
  const editing = drawer && drawer !== 'new' ? drawer : null
  const usedBy = (id: string) => (repos.data ?? []).filter((r) => r.routingRuleId === id).map((r) => r.name)
  const validRe = (s: string) => { try { new RegExp(s); return true } catch { return false } }
  const save = useMutation({
    mutationFn: (v: any) => { const body = { ...v, matchers: matchers.filter(Boolean) }; return drawer === 'new' ? post('routing-rules', body) : put(`routing-rules/${editing!.id}`, body) },
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); qc.invalidateQueries({ queryKey: ['routing-rules'] }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => del(`routing-rules/${id}`), onSuccess: () => { setToDelete(null); qc.invalidateQueries({ queryKey: ['routing-rules'] }); qc.invalidateQueries({ queryKey: ['repositories'] }) }, onError: (e) => message.error(errText(e)) })
  const runTest = async () => {
    try {
      const r = await post<{ allowed: boolean }>('routing-rules/test', { mode: form.getFieldValue('mode'), matchers: matchers.filter(Boolean), path: testPath })
      setResult({ allowed: r.allowed })
    } catch (e) {
      setResult({ error: errText(e) })
    }
  }
  const hits = (m: string) => { try { return new RegExp(m).test(testPath) } catch { return false } }

  return (
    <>
      <PageHeader title={t('nav.routing', 'Routing Rules')} count={rules.data?.length} sub={t('routing.sub', 'Block or allow request paths per repository, e.g. keep a proxy from fetching internal group IDs from the internet.')} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setMatchers(['']); setResult(null); setDrawer('new') }}>{t('routing.create', 'Create rule')}</Button>} />
      <div className="hlk-card" style={{ padding: 0 }}>
        <SortableTable<RoutingRule>
          rowKey="id" loading={rules.isLoading} dataSource={rules.data ?? []} className="hlk-table hlk-clickable" pagination={false} size="middle"
          onRow={(r) => ({ onClick: () => { form.setFieldsValue(r); setMatchers(r.matchers.length ? r.matchers : ['']); setResult(null); setDrawer(r) } })}
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', width: 200, render: (x: string) => <span style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('routing.mode', 'Mode'), dataIndex: 'mode', width: 90, render: (m: string) => <Tag color={m === 'allow' ? 'success' : 'error'}>{m}</Tag> },
            { title: t('routing.matchers', 'Matchers'), dataIndex: 'matchers', width: 120, render: (ms: string[]) => <Tooltip title={<div className="hlk-mono" style={{ fontSize: 11 }}>{ms.map((m) => <div key={m}>{m}</div>)}</div>}><span style={{ fontSize: 12 }}>{ms.length} regex</span></Tooltip> },
            { title: t('common.description', 'Description'), dataIndex: 'description', render: (x: string) => <span style={{ color: 'var(--hlk-text-secondary)' }}>{x}</span> },
            { title: t('routing.usedBy', 'Used by'), sortValue: (r: any) => usedBy(r.id).length, width: 240, render: (_: unknown, r) => usedBy(r.id).map((n) => <Tag key={n} className="hlk-mono" style={{ fontSize: 11 }}>{n}</Tag>) },
            { title: '', width: 80, render: (_: unknown, r) => can('app:repositories', 'delete') && <a style={{ color: 'var(--hlk-error)', fontSize: 12 }} onClick={(e) => { e.stopPropagation(); setToDelete(r) }}>{t('common.delete', 'Delete')}</a> },
          ]}
        />
      </div>
      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={820} title={drawer === 'new' ? t('routing.create', 'Create rule') : t('routing.edit', 'Edit rule')} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <div className="hlk-side-grid">
          <Form form={form} layout="vertical" initialValues={{ mode: 'block' }} onFinish={(v) => save.mutate(v)} disabled={!canWrite}>
            <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }]}><Input /></Form.Item>
            <Form.Item name="description" label={t('common.description', 'Description')}><Input /></Form.Item>
            <Form.Item name="mode" label={t('routing.mode', 'Mode')}>
              <Radio.Group>
                <Radio value="block"><b>block</b> — {t('routing.blockHint', 'reject paths matching any matcher')}</Radio><br />
                <Radio value="allow"><b>allow</b> — {t('routing.allowHint', 'only paths matching a matcher are served')}</Radio>
              </Radio.Group>
            </Form.Item>
            <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 6 }}>{t('routing.matchers', 'Matchers')} <span style={{ fontWeight: 400, color: 'var(--hlk-text-tertiary)' }}>(regex, {t('routing.matchersHint', 'matched against the request path')})</span></div>
            {matchers.map((m, i) => (
              <div key={i} style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
                <Input className="hlk-mono" value={m} status={m && !validRe(m) ? 'error' : undefined} onChange={(e) => setMatchers(matchers.map((x, j) => (j === i ? e.target.value : x)))} placeholder="^/com/acme/.*" style={{ borderColor: result && m && hits(m) ? 'var(--hlk-amber)' : undefined }} />
                <Button type="text" icon={<DeleteOutlined />} onClick={() => setMatchers(matchers.filter((_, j) => j !== i))} disabled={matchers.length === 1} />
              </div>
            ))}
            <Button type="link" size="small" icon={<PlusOutlined />} onClick={() => setMatchers([...matchers, ''])} style={{ paddingLeft: 0, marginBottom: 16 }}>{t('routing.addMatcher', 'Add matcher')}</Button>
            <div style={{ display: 'flex', gap: 8 }}>
              <Button type="primary" htmlType="submit" loading={save.isPending} disabled={!matchers.some(Boolean) || matchers.some((m) => m && !validRe(m))}>{t('common.save', 'Save')}</Button>
              <Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button>
            </div>
          </Form>
          <TestPanel title={t('routing.testPath', 'Test a path')}>
            <Input size="small" className="hlk-mono" value={testPath} onChange={(e) => setTestPath(e.target.value)} style={{ marginBottom: 10 }} />
            <Button size="small" onClick={runTest}>{t('common.test', 'Test')}</Button>
            {result && <div style={{ marginTop: 12, fontSize: 13, fontWeight: 500, color: result.error ? 'var(--hlk-error)' : result.allowed ? 'var(--hlk-success)' : 'var(--hlk-error)' }}>{result.error ?? (result.allowed ? '✓ ' + t('routing.allowed', 'Allowed') : '✗ ' + t('routing.blocked', 'Blocked'))}</div>}
            <div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)', marginTop: 8 }}>{t('routing.hitHint', 'Matching matchers are highlighted in amber.')}</div>
          </TestPanel>
        </div>
      </Drawer>
      <ConfirmDelete open={!!toDelete} name={toDelete?.name ?? ''} typeToConfirm={false} title={t('routing.deleteTitle', 'Delete rule "{{name}}"?', { name: toDelete?.name })} description={toDelete && usedBy(toDelete.id).length ? t('routing.deleteUsed', 'Repositories using it ({{list}}) will have no routing rule afterwards.', { list: usedBy(toDelete.id).join(', ') }) : undefined} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.id) }} loading={remove.isPending} />
    </>
  )
}
