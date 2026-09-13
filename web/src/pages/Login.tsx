import { useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, Button, ConfigProvider, Form, Input } from 'antd'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/auth/AuthContext'
import { ApiError, get } from '@/api/client'
import { LogoMark, Wordmark } from '@/components/Logo'
import { LangSwitch } from '@/layout/AppShell'
import { formatIcons, light } from '@/theme/tokens'
import type { Health, Status } from '@/api/types'
import { useQuery } from '@tanstack/react-query'

function safeNext(v: string | null) {
  if (!v || !v.startsWith('/') || v.startsWith('//')) return '/'
  return v
}

// Deterministic pseudo-random so tiles do not jump between renders.
function seeded(i: number, salt = 0) {
  const x = Math.sin((i + 1) * 12.9898 + salt * 78.233) * 43758.5453
  return x - Math.floor(x)
}

function Tiles() {
  const tiles = useMemo(() => {
    const list = Object.entries(formatIcons)
    const cols = 4
    const rows = Math.ceil((list.length + 1) / cols)
    return list.map(([key, f], i) => {
      const col = i % cols
      const row = Math.floor(i / cols)
      return {
        key, ...f,
        // Percentages so the field always fills the dark panel, whatever its size.
        left: (col + 0.08 + seeded(i, 1) * 0.62) * (100 / cols),
        top: (row + 0.08 + seeded(i, 2) * 0.62) * (100 / rows),
        dur: 2.8 + seeded(i, 3) * 3.6,
        delay: -seeded(i, 4) * 6,
      }
    })
  }, [])
  return (
    <div className="hlk-tiles">
      {tiles.map((tl) => (
        <div
          key={tl.key}
          className="hlk-tile"
          style={{ left: `${tl.left}%`, top: `${tl.top}%`, ['--dur' as any]: `${tl.dur}s`, ['--delay' as any]: `${tl.delay}s`, ['--c' as any]: tl.color }}
        >
          <span className="hlk-face" style={{ background: tl.color, color: tl.fg ?? '#131923' }}>{tl.abbr}</span>
          <span className="hlk-lab">{tl.label}</span>
        </div>
      ))}
      <div className="hlk-tile hlk-tile-hero" style={{ left: '44%', top: '86%', ['--c' as any]: '#E8963A' }}>
        <span className="hlk-face" style={{ background: '#E8963A', color: '#131923', fontSize: 11 }}>好料</span>
      </div>
    </div>
  )
}

export default function Login() {
  const { t } = useTranslation()
  const { methods, session, loading, login, refresh } = useAuth()
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

  if (!loading && session && !session.anonymous)
    return <Navigate to={session.mustChangePassword ? '/change-password?forced=1' : next} replace />

  const onFinish = async (v: { username: string; password: string }) => {
    setSubmitting(true)
    setError(null)
    try {
      const s = await login(v.username.trim(), v.password)
      if (s.mustChangePassword) {
        navigate('/change-password?forced=1', { replace: true })
        return
      }
      // Kept for accounts that predate the flag: the health check spots a
      // password still set to a known default.
      const { makeCan } = await import('@/auth/AuthContext')
      if (makeCan(s).can('app:status', 'read')) {
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

  const version = status.data?.version ? `v${status.data.version}` : ''
  const sellingPoints = [t('login.sp1', '26 formats, one address'), t('login.sp2', 'Single binary, low memory'), t('login.sp3', 'Replace Nexus without touching CI')]

  return (
    <div className="hlk-login">
      <div className="hlk-login-brand">
        <div className="hlk-login-bar" />
        <div className="hlk-login-field"><Tiles /></div>
        <div style={{ position: 'relative', zIndex: 2 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <LogoMark size={40} ink="#F3EFE7" />
            <Wordmark size={30} color="#F3EFE7" />
          </div>
          <div style={{ marginTop: 14, display: 'flex', alignItems: 'baseline', gap: 12 }}>
            <span style={{ fontSize: 22, fontWeight: 700, letterSpacing: '.1em', color: '#F3EFE7' }}>好料庫</span>
            <span className="hlk-mono" style={{ fontSize: 14, color: '#9AA5B5' }}>hó-liāu-khòo</span>
          </div>
        </div>
        <div className="hlk-login-tag" style={{ position: 'relative', zIndex: 2, maxWidth: 480 }}>
          <div style={{ fontSize: 34, fontWeight: 500, color: '#F3EFE7', lineHeight: 1.25, marginBottom: 28, fontFamily: '"Noto Sans TC", system-ui, sans-serif' }}>"the good-stuff store"</div>
          <div style={{ display: 'grid', gap: 14 }}>
            {sellingPoints.map((s) => (
              <div key={s} style={{ display: 'flex', alignItems: 'center', gap: 12, fontSize: 16, color: '#C9CFD8' }}>
                <span style={{ width: 8, height: 8, background: '#E8963A', borderRadius: 2, flex: 'none' }} />
                {s}
              </div>
            ))}
          </div>
        </div>
        <div className="hlk-mono" style={{ position: 'relative', zIndex: 2, fontSize: 12, color: '#6F7A8A' }}>{version}</div>
      </div>

      <ConfigProvider theme={light}>
        <div className="hlk-login-form">
          <div className="hlk-login-form-inner">
            <div className="hlk-login-brandsm">
              <LogoMark size={26} ink="#F3EFE7" />
              <Wordmark size={18} color="#F3EFE7" />
            </div>
            <h1 style={{ fontSize: 24, fontWeight: 600, margin: '0 0 24px', color: '#1E2A3B' }}>{t('login.title', 'Log in to Holiaokho')}</h1>
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
                <div style={{ display: 'flex', alignItems: 'center', gap: 12, margin: '18px 0', color: '#8A94A3', fontSize: 12 }}>
                  <div style={{ flex: 1, height: 1, background: '#E3DFD6' }} />
                  {t('login.or', 'or')}
                  <div style={{ flex: 1, height: 1, background: '#E3DFD6' }} />
                </div>
                <Button block style={{ height: 42 }} href={`${methods.oidcLoginUrl}?next=${encodeURIComponent('/ui' + next)}`}>
                  {t('login.sso', 'Log in with SSO')}
                </Button>
              </>
            )}
            {methods?.anonymous && (
              <div style={{ marginTop: 20, fontSize: 13 }}>
                <Link to="/browse" onClick={() => refresh()} style={{ color: '#35558A' }}>{t('login.browseAnon', 'Browse packages without logging in →')}</Link>
              </div>
            )}
            <div style={{ marginTop: 20, fontSize: 12, color: '#8A94A3' }}>{t('login.forgot', 'Forgot your password? Ask an administrator to reset it.')}</div>
          </div>
          <div className="hlk-login-foot">
            <span className="hlk-mono" style={{ fontSize: 12, color: '#8A94A3' }}>{version}</span>
            <LangSwitch />
          </div>
        </div>
      </ConfigProvider>
    </div>
  )
}
