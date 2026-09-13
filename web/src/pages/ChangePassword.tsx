import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, App, Button, Card, Form, Input } from 'antd'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/auth/AuthContext'
import { ApiError, put } from '@/api/client'
import { PasswordRules, passwordOk } from '@/components/PasswordRules'
import { LogoMark, Wordmark } from '@/components/Logo'
import { useErrorText } from '@/components/Common'

export default function ChangePassword() {
  const { t } = useTranslation()
  const { methods, session, refresh } = useAuth()
  const [params] = useSearchParams()
  const forced = params.get('forced') === '1'
  const navigate = useNavigate()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [failedCode, setFailedCode] = useState<string | undefined>()
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const policy = methods?.passwordPolicy
  const ok = passwordOk(policy, pw, session?.username) && pw.length > 0 && pw === pw2

  const onFinish = async (v: { current: string }) => {
    setBusy(true)
    setError(null)
    setFailedCode(undefined)
    try {
      await put('me/password', { current: v.current, password: pw })
      message.success(t('password.changed', 'Password changed'))
      await refresh()
      navigate('/', { replace: true })
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setError(t('password.currentWrong', 'Current password is incorrect'))
      else if (e instanceof ApiError && e.code.startsWith('password.')) {
        setFailedCode(e.code)
        setError(errText(e))
      } else setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: 'var(--hlk-bg)', padding: 24 }}>
      <Card style={{ width: 440, borderRadius: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 20 }}>
          <LogoMark size={26} ink="var(--hlk-text)" />
          <Wordmark size={18} />
        </div>
        <h1 style={{ fontSize: 20, fontWeight: 600, margin: '0 0 6px' }}>{forced ? t('password.forcedTitle', 'Please change the default password first') : t('password.title', 'Change password')}</h1>
        <p style={{ color: 'var(--hlk-text-secondary)', fontSize: 13, margin: '0 0 20px' }}>
          {forced ? t('password.forcedHint', 'The admin account still uses the default password. Set a new one before continuing.') : t('password.hint', 'Choose a new password for {{user}}.', { user: session?.username })}
        </p>
        {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
        <Form layout="vertical" onFinish={onFinish} requiredMark={false} disabled={busy}>
          <Form.Item name="current" label={t('password.current', 'Current password')} rules={[{ required: true, message: t('password.currentRequired', 'Enter your current password') }]}>
            <Input.Password autoComplete="current-password" autoFocus />
          </Form.Item>
          <Form.Item label={t('password.new', 'New password')} required>
            <Input.Password value={pw} onChange={(e) => { setPw(e.target.value); setFailedCode(undefined) }} autoComplete="new-password" />
            <PasswordRules policy={policy} value={pw} username={session?.username} failedCode={failedCode} />
          </Form.Item>
          <Form.Item label={t('password.confirm', 'Confirm new password')} required validateStatus={pw2 && pw2 !== pw ? 'error' : undefined} help={pw2 && pw2 !== pw ? t('password.mismatch', 'Passwords do not match') : undefined}>
            <Input.Password value={pw2} onChange={(e) => setPw2(e.target.value)} autoComplete="new-password" />
          </Form.Item>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            {!forced && <Button onClick={() => navigate(-1)}>{t('common.cancel', 'Cancel')}</Button>}
            <Button type="primary" htmlType="submit" disabled={!ok} loading={busy}>{t('password.submit', 'Change password')}</Button>
          </div>
        </Form>
      </Card>
    </div>
  )
}
