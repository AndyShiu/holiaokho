import { useEffect, useState } from 'react'
import { Alert, App, Button, Form, Input, InputNumber, Radio, Select, Skeleton, Switch } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post, put } from '@/api/client'
import type { EmailSettings } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, SecretHint, useErrorText } from '@/components/Common'

export default function Email() {
  const { t } = useTranslation()
  const { can, session } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [form] = Form.useForm()
  const [testTo, setTestTo] = useState('')
  const q = useQuery({ queryKey: ['email'], queryFn: () => get<EmailSettings>('email') })
  useEffect(() => { if (q.data) form.setFieldsValue({ ...q.data, password: q.data.password ? '***' : '', tls: q.data.ssl ? 'ssl' : q.data.startTls ? 'starttls' : 'none' }) }, [q.data, form])
  const canWrite = can('app:system', 'write')
  const save = useMutation({
    mutationFn: (v: any) => { const body = { ...v, startTls: v.tls === 'starttls', ssl: v.tls === 'ssl' }; delete body.tls; if (body.password === '***' || body.password === '') delete body.password; return put('email', body) },
    onSuccess: () => { message.success(t('common.saved', 'Saved')); qc.invalidateQueries({ queryKey: ['email'] }) },
  })
  const test = useMutation({ mutationFn: () => post<{ sent: boolean }>('email/test', { to: testTo }), onSuccess: () => message.success(t('email.testSent', 'Test email sent')), onError: (e) => message.error(errText(e)) })
  if (q.isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader title={t('nav.email', 'Email')} sub={t('email.sub', 'Outgoing SMTP for task-failure and quota notifications.')} />
      <div className="hlk-card" style={{ maxWidth: 720 }}>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} disabled={!canWrite}>
          <Form.Item name="enabled" label={t('common.enabled', 'Enabled')} valuePropName="checked"><Switch /></Form.Item>
          <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 12 }}>
            <Form.Item name="host" label="Host"><Input className="hlk-mono" placeholder="smtp.example.com" /></Form.Item>
            <Form.Item name="port" label="Port"><InputNumber min={1} max={65535} style={{ width: '100%' }} /></Form.Item>
            <Form.Item name="username" label={t('login.username', 'Username')}><Input autoComplete="off" /></Form.Item>
            <Form.Item name="password" label={t('login.password', 'Password')} extra={<SecretHint />}><Input.Password autoComplete="new-password" /></Form.Item>
          </div>
          <Form.Item name="from" label="From"><Input className="hlk-mono" placeholder="holiaokho@example.com" /></Form.Item>
          <Form.Item name="tls" label={t('email.encryption', 'Encryption')}><Radio.Group options={[{ value: 'none', label: t('common.none', 'None') }, { value: 'starttls', label: 'STARTTLS' }, { value: 'ssl', label: 'SSL/TLS' }]} /></Form.Item>
          <Form.Item name="recipients" label={t('email.recipients', 'Notification recipients')} extra={t('email.recipientsHint', 'Receive task failures and storage quota warnings.')}><Select mode="tags" tokenSeparators={[',', ' ']} className="hlk-mono" /></Form.Item>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Button type="primary" htmlType="submit" loading={save.isPending}>{t('common.save', 'Save')}</Button>
            <div style={{ flex: 1 }} />
            <Input size="small" placeholder={session?.username ? `${session.username}@…` : 'you@example.com'} value={testTo} onChange={(e) => setTestTo(e.target.value)} style={{ width: 220 }} />
            <Button size="small" loading={test.isPending} disabled={!testTo} onClick={() => test.mutate()}>{t('email.sendTest', 'Send test email')}</Button>
          </div>
          <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginTop: 8, textAlign: 'right' }}>{t('email.testHint', 'The test uses the saved settings — save first.')}</div>
        </Form>
      </div>
    </>
  )
}
