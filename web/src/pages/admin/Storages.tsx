import { useState } from 'react'
import { Alert, App, Button, Checkbox, Drawer, Form, Input, InputNumber, Progress, Radio, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post, put } from '@/api/client'
import type { Health, Storage } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, SecretHint, useErrorText } from '@/components/Common'
import { fmtBytes, StatusDot } from '@/components/Format'

const GB = 1024 ** 3

function TestButton({ getValues }: { getValues: () => Partial<Storage> }) {
  const { t } = useTranslation()
  const [res, setRes] = useState<{ ok: boolean; message?: string; latencyMs?: number } | null>(null)
  const [busy, setBusy] = useState(false)
  return (
    <span style={{ display: 'inline-flex', gap: 10, alignItems: 'center' }}>
      <Button loading={busy} onClick={async () => { setBusy(true); try { setRes(await post('storages/test', getValues())) } catch (e: any) { setRes({ ok: false, message: e?.message }) } finally { setBusy(false) } }}>{t('storages.test', 'Test connection')}</Button>
      {res && (res.ok ? <span style={{ color: 'var(--hlk-success)', fontSize: 12 }}>✓ {t('storages.testOk', 'OK · {{ms}} ms', { ms: res.latencyMs })}</span> : <span style={{ color: 'var(--hlk-error)', fontSize: 12 }}>{res.message}</span>)}
    </span>
  )
}

export default function Storages() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [open, setOpen] = useState<'new' | Storage | null>(null)
  const [form] = Form.useForm()
  const storages = useQuery({ queryKey: ['storages'], queryFn: () => get<Storage[]>('storages') })
  const health = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check'), enabled: can('app:status', 'read') })
  const canWrite = can('app:storages', 'write')
  const save = useMutation({
    mutationFn: async (v: any) => {
      if (open === 'new') return post('storages', { name: v.name, type: v.type, config: v.config, quotaBytes: Math.round((v.quotaGb ?? 0) * GB) })
      const cfg = { ...v.config }
      if (cfg.secretKey === '***' || cfg.secretKey === '') delete cfg.secretKey
      return put(`storages/${(open as Storage).name}`, { quotaBytes: Math.round((v.quotaGb ?? 0) * GB), config: cfg })
    },
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setOpen(null); qc.invalidateQueries({ queryKey: ['storages'] }); qc.invalidateQueries({ queryKey: ['health'] }) },
  })
  const editing = open && open !== 'new' ? open : null
  const check = (name: string) => health.data?.checks?.[`storage:${name}`]

  return (
    <>
      <PageHeader title={t('nav.storages', 'Storages')} count={storages.data?.length} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setOpen('new') }}>{t('storages.create', 'Create Storage')}</Button>} />
      <div className="hlk-card" style={{ padding: 0, marginBottom: 16 }}>
        <Table<Storage>
          rowKey="id" loading={storages.isLoading} dataSource={storages.data ?? []} className="hlk-table" pagination={false} size="middle"
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', render: (x: string) => <span className="hlk-mono" style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('common.type', 'Type'), dataIndex: 'type', width: 80, render: (x: string) => <Tag>{x}</Tag> },
            { title: t('storages.location', 'Location'), render: (_: unknown, s) => <span className="hlk-mono" style={{ fontSize: 12 }}>{s.type === 'fs' ? s.config?.path : `${s.config?.bucket}${s.config?.prefix ? '/' + s.config.prefix : ''} @ ${s.config?.endpoint || s.config?.region || 's3'}`}</span> },
            {
              title: t('storages.usage', 'Usage'), width: 260, render: (_: unknown, s) => {
                const c = check(s.name)
                if (!c) return <span style={{ color: 'var(--hlk-text-tertiary)' }}>—</span>
                const pct = s.quotaBytes ? Math.min(100, Math.round(((c.usedBytes ?? 0) / s.quotaBytes) * 100)) : undefined
                return (
                  <div>
                    <div style={{ fontSize: 12 }}>{fmtBytes(c.usedBytes)}{s.quotaBytes ? ` / ${fmtBytes(s.quotaBytes)}` : ` · ${t('storages.noQuota', 'no quota')}`}</div>
                    {pct !== undefined && <Progress percent={pct} size={['100%', 6]} showInfo={false} strokeColor={pct >= 100 ? 'var(--hlk-error)' : pct >= 90 ? 'var(--hlk-warning)' : 'var(--hlk-ink)'} />}
                  </div>
                )
              },
            },
            { title: t('common.status', 'Status'), width: 200, render: (_: unknown, s) => { const c = check(s.name); return c ? <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', fontSize: 12 }}><StatusDot status={!c.healthy ? 'error' : c.message ? 'warning' : 'success'} />{c.message ?? t('health.ok', 'OK')}</span> : null } },
            { title: '', width: 80, render: (_: unknown, s) => canWrite && <a onClick={() => { form.setFieldsValue({ name: s.name, type: s.type, config: { ...s.config, secretKey: s.config?.secretKey ? '***' : '' }, quotaGb: s.quotaBytes ? +(s.quotaBytes / GB).toFixed(2) : 0 }); setOpen(s) }}>{t('common.edit', 'Edit')}</a> },
          ]}
        />
      </div>
      <Alert type="info" showIcon message={t('storages.dedupTitle', 'Content-addressed storage')} description={t('storages.dedupHint', 'A file that exists in several repositories is stored once. Deleted content is released by the blob-gc task; compact-blobs then frees the disk space.')} />
      <Drawer open={!!open} onClose={() => setOpen(null)} width={600} title={open === 'new' ? t('storages.create', 'Create Storage') : t('storages.edit', 'Edit {{name}}', { name: editing?.name })} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <Form form={form} layout="vertical" initialValues={{ type: 'fs', config: { pathStyle: true, region: 'us-east-1' }, quotaGb: 0 }} onFinish={(v) => save.mutate(v)}>
          <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }, { pattern: /^[A-Za-z0-9._-]+$/, message: t('repos.nameInvalid', 'Only letters, digits, . _ -') }]}><Input className="hlk-mono" disabled={!!editing} /></Form.Item>
          <Form.Item name="type" label={t('common.type', 'Type')}>
            <Radio.Group disabled={!!editing} options={[{ value: 'fs', label: t('storages.fs', 'Local filesystem') }, { value: 's3', label: t('storages.s3', 'S3-compatible object storage') }]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.type !== b.type}>
            {({ getFieldValue }) =>
              getFieldValue('type') === 'fs' ? (
                <Form.Item name={['config', 'path']} label={t('storages.path', 'Path')} rules={[{ required: true }]} extra={t('storages.pathHint', 'Absolute path on the server (inside the container when deployed with Docker/K8s).')}><Input className="hlk-mono" disabled={!!editing} placeholder="/data/blobs" /></Form.Item>
              ) : (
                <>
                  <div className="hlk-form-row" style={{ gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                    <Form.Item name={['config', 'endpoint']} label={t('storages.endpoint', 'Endpoint')} extra={t('storages.endpointHint', 'Empty = AWS')}><Input className="hlk-mono" placeholder="https://s3.example.com" /></Form.Item>
                    <Form.Item name={['config', 'region']} label={t('storages.region', 'Region')}><Input className="hlk-mono" /></Form.Item>
                    <Form.Item name={['config', 'bucket']} label={t('storages.bucket', 'Bucket')} rules={[{ required: true }]}><Input className="hlk-mono" disabled={!!editing} /></Form.Item>
                    <Form.Item name={['config', 'prefix']} label={t('storages.prefix', 'Prefix')}><Input className="hlk-mono" disabled={!!editing} /></Form.Item>
                    <Form.Item name={['config', 'accessKey']} label={t('storages.accessKey', 'Access key')}><Input className="hlk-mono" autoComplete="off" /></Form.Item>
                    <Form.Item name={['config', 'secretKey']} label={t('storages.secretKey', 'Secret key')} extra={editing && <SecretHint />}><Input.Password autoComplete="new-password" /></Form.Item>
                  </div>
                  <Form.Item name={['config', 'pathStyle']} valuePropName="checked"><Checkbox>{t('storages.pathStyle', 'Path-style addressing (MinIO and most self-hosted S3)')}</Checkbox></Form.Item>
                </>
              )
            }
          </Form.Item>
          <Form.Item name="quotaGb" label={t('storages.quota', 'Quota')} extra={t('storages.quotaHint', '0 = unlimited. Uploads are rejected with 507 when exceeded; a warning shows above 90%.')}><InputNumber min={0} step={10} addonAfter="GB" style={{ width: 200 }} /></Form.Item>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
            <Button type="primary" htmlType="submit" loading={save.isPending}>{open === 'new' ? t('common.create', 'Create') : t('common.save', 'Save')}</Button>
            <Button onClick={() => setOpen(null)}>{t('common.cancel', 'Cancel')}</Button>
            <div style={{ flex: 1 }} />
            <TestButton getValues={() => { const v = form.getFieldsValue(); return { name: editing?.name ?? v.name, type: v.type, config: v.config } }} />
          </div>
        </Form>
      </Drawer>
    </>
  )
}
