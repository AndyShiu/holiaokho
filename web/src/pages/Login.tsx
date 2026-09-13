import { useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, Button, Form, Input } from 'antd'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/auth/AuthContext'
import { ApiError, get } from '@/api/client'
import { LogoMark, Wordmark } from '@/components/Logo'
import { LangSwitch } from '@/layout/AppShell'
import { formatIcons } from '@/theme/tokens'
import type { Health, Status } from '@/api/types'
import { useQuery } from '@tanstack/react-query'

function safeNext(v: string | null) {
  if (!v || !v.startsWith('/') || v.startsWith('//')) return '/'
  return v
}

// Deterministic pseudo-random so tiles do not jump between renders.
function seeded(i: number) {
  const x = Math.sin(i * 9301 + 49297) * 233280
  return x - Math.floor(x)
}

function Tiles() {
  const tiles = useMemo(() => {
    const list = Object.entries(formatIcons)
    const cols = 6
    return list.map(([key, f], i) => {
      const col = i % cols
      const row = Math.floor(i / cols)
      return {
        key, ...f,
        left: 40 + col * 66 + seeded(i) * 18,
        top: 40 + row * 66 + seeded(i + 100) * 18,
        dur: 2.8 + seeded(i + 200) * 3.6,
        delay: -seeded(i + 300) * 6,
      }
    })
  }, [])
  return (
    <div className="hlk-tiles" style={{ position: 'absolute', left: 580, top: 60, right: 0, bottom: 0, overflow: 'hidden', pointerEvents: 'auto' }} aria-hidden>
      {tiles.map((tl) => (
        <div key={tl.key} className="hlk-tile" style={{ left: tl.left, top: tl.top, background: tl.color, color: tl.fg ?? '#131923', ['--dur' as any]: `${tl.dur}s`, ['--delay' as any]: `${tl.delay}s`, ['--tile' as any]: tl.color }}>
          {tl.abbr}
          <span className="hlk-tile-label">{tl.label}</span>
        </div>
      ))}
      <div className="hlk-tile" style={{ left: 40 + 2 * 66 + 33, top: 40 + 5 * 66, background: '#E8963A', color: '#131923', animation: 'hlk-breathe 4s ease-in-out infinite', fontSize: 11 }}>好料</div>
    </div>
  )
}

export default function Login() {
  const { t } = useTranslation()
  const { methods, session, loading, login, can, refresh } = useAuth()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const next = safeNext(params.get('next'))
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [lockUntil, setLockUntil] = useState<number>(0)
  const [now, setNow] = useState(Date.now())
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status'), staleTime: 300000 })

  useEffect(() => {
    if (!lockUntil) return
    const id = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(id)
  }, [lockUntil])
  useEffect(() => {
    const err = params.get('error')
    if (err) setError(t('login.ssoFailed', 'SSO login failed: {{reason}}', { reason: err }))
  }, [params, t])

  const locked = lockUntil > now
  const remaining = Math.ceil((lockUntil - now) / 1000)

  if (!loading && session && !session.anonymous) return <Navigate to={next} replace />

  const onFinish = async (v: { username: string; password: string }) => {
    setSubmitting(true)
    setError(null)
    try {
      const s = await login(v.username.trim(), v.password)
      const { can: canNow } = (await import('@/auth/AuthContext')).makeCan(s)
      if (canNow('app:status', 'read')) {
        try {
          const h = await get<Health>('status/check')
          if (h.checks?.default_admin_password && !h.checks.default_admin_password.healthy && s.username === 'admin') {
            navigate('/change-password?forced=1', { replace: true })
            return
          }
        } catch {}
      }
      navigate(next, { replace: true })
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setError(t('login.badCredentials', 'Incorrect username or password'))
      else if (e instanceof ApiError && e.status === 429) {
        setError(t('login.rateLimited', 'Too many failed attempts. Please wait 30 seconds.'))
        setLockUntil(Date.now() + 30000)
      } else setError(t('login.unreachable', 'Cannot reach the server'))
    } finally {
      setSubmitting(false)
    }
  }

  const sellingPoints = [t('login.sp1', '26 formats, one address'), t('login.sp2', 'Single binary, low memory'), t('login.sp3', 'Replace Nexus without touching CI')]

  return (
    <div className="hlk-login" style={{ minHeight: '100vh', display: 'grid', gridTemplateColumns: 'minmax(0,1fr) 520px' }}>
      <style>{`@media (max-width:1024px){.hlk-login{grid-template-columns:1fr !important}.hlk-login-brand{min-height:auto !important;padding:22px 32px !important}.hlk-login-brand .hlk-tiles,.hlk-login-brand .hlk-sp{display:none}.hlk-login-form{padding:32px 24px !important}}`}</style>
      <div className="hlk-login-brand" style={{ background: '#131923', color: '#E9E6DF', padding: '56px 64px', position: 'relative', display: 'flex', flexDirection: 'column', justifyContent: 'space-between', overflow: 'hidden', minHeight: '100vh' }}>
        <div style={{ position: 'absolute', top: 0, left: 64, width: 160, height: 3, background: '#E8963A', borderRadius: '0 0 2px 2px' }} />
        <Tiles />
        <div style={{ position: 'relative', zIndex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <LogoMark size={40} ink="#F3EFE7" />
            <Wordmark size={30} color="#F3EFE7" />
          </div>
          <div style={{ marginTop: 14, display: 'flex', alignItems: 'baseline', gap: 12 }}>
            <span style={{ fontSize: 22, fontWeight: 700, letterSpacing: '.1em' }}>好料庫</span>
            <span className="hlk-mono" style={{ fontSize: 14, color: '#9AA5B5' }}>hó-liāu-khòo</span>
          </div>
        </div>
        <div className="hlk-sp" style={{ position: 'relative', zIndex: 1, maxWidth: 480 }}>
          <div style={{ fontSize: 34, fontWeight: 500, color: '#F3EFE7', lineHeight: 1.25, marginBottom: 28 }}>"the good-stuff store"</div>
          <div style={{ display: 'grid', gap: 12 }}>
            {sellingPoints.map((s) => (
              <div key={s} style={{ display: 'flex', alignItems: 'center', gap: 12, fontSize: 16, color: '#C9CFD8' }}>
                <span style={{ width: 8, height: 8, background: '#E8963A', borderRadius: 2, flex: 'none' }} />
                {s}
              </div>
            ))}
          </div>
        </div>
        <div className="hlk-mono" style={{ position: 'relative', zIndex: 1, fontSize: 12, color: '#6F7A8A' }}>{status.data?.version ? `v${status.data.version}` : ''}</div>
      </div>
      <div className="hlk-login-form" style={{ background: 'var(--hlk-bg)', padding: '0 64px', display: 'flex', flexDirection: 'column', justifyContent: 'center', position: 'relative' }}>
        <div style={{ width: '100%', maxWidth: 400, margin: '0 auto' }}>
          <h1 style={{ fontSize: 24, fontWeight: 600, margin: '0 0 24px' }}>{t('login.title', 'Log in to Holiaokho')}</h1>
          {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
          <Form layout="vertical" onFinish={onFinish} disabled={submitting || locked} requiredMark={false} size="large">
            <Form.Item name="username" label={t('login.username', 'Username')} rules={[{ required: true, message: t('login.usernameRequired', 'Enter your username') }]}>
              <Input autoFocus autoComplete="username" spellCheck={false} />
            </Form.Item>
            <Form.Item name="password" label={t('login.password', 'Password')} rules={[{ required: true, message: t('login.passwordRequired', 'Enter your password') }]}>
              <Input.Password autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={submitting} style={{ height: 42, marginTop: 4 }}>
              {locked ? t('login.retryIn', 'Retry in {{s}}s', { s: remaining }) : t('login.submit', 'Log in')}
            </Button>
          </Form>
          {methods?.oidc && (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, margin: '18px 0', color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>
                <div style={{ flex: 1, height: 1, background: 'var(--hlk-border)' }} />
                {t('login.or', 'or')}
                <div style={{ flex: 1, height: 1, background: 'var(--hlk-border)' }} />
              </div>
              <Button block style={{ height: 42 }} href={`${methods.oidcLoginUrl}?next=${encodeURIComponent('/ui' + next)}`}>
                {t('login.sso', 'Log in with SSO')}
              </Button>
            </>
          )}
          {methods?.anonymous && (
            <div style={{ marginTop: 20, fontSize: 13 }}>
              <Link to="/browse" onClick={() => refresh()}>{t('login.browseAnon', 'Browse packages without logging in →')}</Link>
            </div>
          )}
          <div style={{ marginTop: 20, fontSize: 12, color: 'var(--hlk-text-tertiary)' }}>{t('login.forgot', 'Forgot your password? Ask an administrator to reset it.')}</div>
        </div>
        <div style={{ position: 'absolute', left: 64, right: 64, bottom: 24, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span className="hlk-mono" style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)' }}>{status.data?.version ? `v${status.data.version}` : ''}</span>
          <LangSwitch />
        </div>
      </div>
    </div>
  )
}
