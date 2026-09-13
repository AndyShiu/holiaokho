import { useState } from 'react'
import { Alert, App, Button, Checkbox, DatePicker, Form, Input, Modal, Radio, Table } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import dayjs, { type Dayjs } from 'dayjs'
import { del, get, post } from '@/api/client'
import type { Token } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, useErrorText } from '@/components/Common'
import { Copyable } from '@/components/Copyable'
import { RelTime } from '@/components/Format'

export function TokenList({ base }: { base: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [open, setOpen] = useState(false)
  const [secret, setSecret] = useState<{ name: string; secret: string; expiresAt?: string | null } | null>(null)
  const [saved, setSaved] = useState(false)
  const [form] = Form.useForm()
  const tokens = useQuery({ queryKey: ['tokens', base], queryFn: () => get<Token[]>(base) })
  const create = useMutation({
    mutationFn: (v: any) => {
      let expiresAt: string | undefined
      if (v.expiry === 'custom' && v.date) expiresAt = (v.date as Dayjs).endOf('day').toISOString()
      else if (v.expiry && v.expiry !== 'never') expiresAt = dayjs().add(Number(v.expiry), 'day').toISOString()
      return post<{ token: Token; secret: string }>(base, { name: v.name, expiresAt })
    },
    onSuccess: (r, v) => { setOpen(false); setSaved(false); setSecret({ name: v.name, secret: r.secret, expiresAt: r.token?.expiresAt }); qc.invalidateQueries({ queryKey: ['tokens', base] }) },
    onError: (e) => message.error(errText(e)),
  })
  const revoke = useMutation({ mutationFn: (id: string) => del(`${base}/${id}`), onSuccess: () => { message.success(t('tokens.revoked', 'Token revoked')); qc.invalidateQueries({ queryKey: ['tokens', base] }) }, onError: (e) => message.error(errText(e)) })
  const expired = (x: Token) => !!x.expiresAt && new Date(x.expiresAt) < new Date()
  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setOpen(true) }}>{t('tokens.create', 'Create token')}</Button>
      </div>
      <Table<Token>
        rowKey="id" loading={tokens.isLoading} dataSource={tokens.data ?? []} className="hlk-table" pagination={false} size="middle"
        rowClassName={(x) => (expired(x) ? 'hlk-expired' : '')}
        columns={[
          { title: t('common.name', 'Name'), dataIndex: 'name', render: (x: string, r) => <span style={{ fontWeight: 500, color: expired(r) ? 'var(--hlk-text-tertiary)' : undefined }}>{x}</span> },
          { title: t('tokens.prefix', 'Prefix'), dataIndex: 'prefix', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}…</span> },
          { title: t('common.created', 'Created'), dataIndex: 'createdAt', width: 130, render: (x: string) => <RelTime value={x} /> },
          { title: t('tokens.lastUsed', 'Last used'), dataIndex: 'lastUsedAt', width: 130, render: (x: string) => <RelTime value={x} empty={t('common.never', 'never')} /> },
          { title: t('tokens.expires', 'Expires'), dataIndex: 'expiresAt', width: 160, render: (x: string, r) => (x ? <span style={{ color: expired(r) ? 'var(--hlk-text-tertiary)' : undefined }}>{expired(r) ? t('tokens.expired', 'expired') + ' · ' : ''}{dayjs(x).format('YYYY-MM-DD')}</span> : t('tokens.never', 'never')) },
          { title: '', width: 80, render: (_: unknown, r) => <a style={{ color: 'var(--hlk-error)', fontSize: 12 }} onClick={() => Modal.confirm({ title: t('tokens.revokeTitle', 'Revoke token "{{name}}"?', { name: r.name }), content: t('tokens.revokeHint', 'Clients using it will start failing immediately.'), okButtonProps: { danger: true }, onOk: () => revoke.mutate(r.id) })}>{t('tokens.revoke', 'Revoke')}</a> },
        ]}
      />
      <Modal open={open} onCancel={() => setOpen(false)} title={t('tokens.create', 'Create token')} okText={t('common.create', 'Create')} onOk={() => form.submit()} okButtonProps={{ loading: create.isPending }} destroyOnClose>
        <Form form={form} layout="vertical" initialValues={{ expiry: '90' }} onFinish={(v) => create.mutate(v)} style={{ marginTop: 12 }}>
          <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }]} extra={t('tokens.nameHint', 'e.g. "gitlab-ci", "laptop"')}><Input autoFocus /></Form.Item>
          <Form.Item name="expiry" label={t('tokens.expires', 'Expires')}>
            <Radio.Group options={[{ value: '30', label: t('tokens.d30', '30 days') }, { value: '90', label: t('tokens.d90', '90 days') }, { value: '365', label: t('tokens.d365', '1 year') }, { value: 'never', label: t('tokens.never', 'never') }, { value: 'custom', label: t('tokens.custom', 'custom') }]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>{({ getFieldValue }) => getFieldValue('expiry') === 'custom' && <Form.Item name="date" label={t('tokens.date', 'Date')} rules={[{ required: true }]}><DatePicker disabledDate={(d) => d.isBefore(dayjs(), 'day')} /></Form.Item>}</Form.Item>
        </Form>
      </Modal>
      <Modal open={!!secret} closable={false} footer={null} width={560} maskClosable={false}>
        {secret && (
          <>
            <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 16 }}>
              <span style={{ width: 32, height: 32, borderRadius: '50%', background: 'var(--hlk-success-bg)', color: 'var(--hlk-success)', display: 'grid', placeItems: 'center', fontWeight: 700 }}>✓</span>
              <span style={{ fontSize: 16, fontWeight: 600 }}>{t('tokens.createdTitle', 'Token "{{name}}" created', { name: secret.name })}</span>
            </div>
            <div style={{ background: '#131923', borderRadius: 10, padding: 16, position: 'relative' }}>
              <div className="hlk-section-label" style={{ color: '#6F7A8A', marginBottom: 8 }}>{t('tokens.secretLabel', 'SECRET · shown only once')}</div>
              <Copyable text={secret.secret} block style={{ color: '#F3EFE7', fontSize: 17, fontWeight: 500, whiteSpace: 'normal', wordBreak: 'break-all' }} />
            </div>
            <Alert type="error" showIcon style={{ margin: '16px 0' }} message={t('tokens.secretWarn', 'You will not be able to see this secret again. Store it now, e.g. as a CI secret variable (HLK_TOKEN).')} description={secret.expiresAt ? t('tokens.expiresOn', 'Expires {{date}}', { date: dayjs(secret.expiresAt).format('YYYY-MM-DD') }) : t('tokens.neverExpires', 'Never expires')} />
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <Checkbox checked={saved} onChange={(e) => setSaved(e.target.checked)}>{t('tokens.saved', 'I have saved it')}</Checkbox>
              <Button type="primary" disabled={!saved} onClick={() => setSecret(null)}>{t('common.close', 'Close')}</Button>
            </div>
          </>
        )}
      </Modal>
    </>
  )
}

export default function Tokens() {
  const { t } = useTranslation()
  const { session } = useAuth()
  const host = window.location.host
  const ex = (s: string) => <code style={{ background: 'var(--hlk-bg)', padding: '2px 6px', borderRadius: 4, fontSize: 12 }}>{s}</code>
  return (
    <>
      <PageHeader title={t('nav.tokens', 'My Tokens')} sub={t('tokens.sub', 'Personal access tokens for CI and command-line clients. They act as {{user}}.', { user: session?.username })} />
      <div className="hlk-card" style={{ marginBottom: 16 }}><TokenList base="me/tokens" /></div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
        <div className="hlk-card"><div className="hlk-section-label" style={{ marginBottom: 8 }}>{t('tokens.useBasic', 'Basic (any client)')}</div><div style={{ fontSize: 13, lineHeight: 1.8 }}>{t('tokens.useBasicHint', 'Username + the token as the password.')}<br />{ex(`curl -u ${session?.username}:hlk_… https://${host}/repository/…`)}</div></div>
        <div className="hlk-card"><div className="hlk-section-label" style={{ marginBottom: 8 }}>{t('tokens.useHeader', 'HTTP header')}</div><div style={{ fontSize: 13, lineHeight: 1.8 }}>{ex('Authorization: Bearer hlk_…')}</div></div>
        <div className="hlk-card"><div className="hlk-section-label" style={{ marginBottom: 8 }}>npm / NuGet / cargo</div><div style={{ fontSize: 13, lineHeight: 1.8 }}>{ex('//host/repository/npm/:_authToken=hlk_…')}<br />{ex('nuget push -ApiKey hlk_…')}<br />{ex('cargo login "Bearer hlk_…"')}</div></div>
      </div>
    </>
  )
}
