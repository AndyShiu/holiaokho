import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, onUnauthorized } from '@/api/client'
import type { AuthMethods, Session } from '@/api/types'

interface AuthState {
  methods: AuthMethods | null
  session: Session | null
  loading: boolean
  refresh: () => Promise<Session | null>
  login: (username: string, password: string) => Promise<Session>
  logout: () => Promise<void>
  can: (target: string, action: string) => boolean
  canRepo: (name: string, format: string, action: string) => boolean
  isAnonymous: boolean
  isLocalUser: boolean
}

const Ctx = createContext<AuthState | null>(null)

// Mirrors internal/auth/rbac.go: exact target, "*", or a "<prefix>:*" wildcard.
export function makeCan(session: Session | null) {
  const privs = session?.privileges ?? []
  const has = (target: string, action: string) =>
    privs.some((p) => {
      const t = p.target
      const tOk = t === '*' || t === target || (t.endsWith(':*') && target.startsWith(t.slice(0, -1)))
      const aOk = p.actions.includes('*') || p.actions.includes(action) || (action === 'read' && p.actions.includes('admin'))
      return tOk && aOk
    })
  return {
    can: has,
    canRepo: (name: string, format: string, action: string) => has(`repo:${name}`, action) || has(`format:${format}`, action),
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [methods, setMethods] = useState<AuthMethods | null>(null)
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const s = await api<Session>('session', { silent401: true })
      setSession(s)
      return s
    } catch {
      setSession(null)
      return null
    }
  }, [])

  useEffect(() => {
    ;(async () => {
      try {
        const m = await api<AuthMethods>('auth/methods')
        setMethods(m)
      } catch {
        setMethods(null)
      }
      await refresh()
      setLoading(false)
    })()
  }, [refresh])

  useEffect(() => onUnauthorized(() => setSession((s) => (s && !s.anonymous ? null : s))), [])

  const login = useCallback(
    async (username: string, password: string) => {
      await api('session', { method: 'POST', body: { username, password }, silent401: true })
      const s = await refresh()
      if (!s) throw new Error('session')
      return s
    },
    [refresh],
  )
  const logout = useCallback(async () => {
    try {
      await api('session', { method: 'DELETE' })
    } finally {
      await refresh()
    }
  }, [refresh])

  const value = useMemo<AuthState>(() => {
    const { can, canRepo } = makeCan(session)
    return {
      methods, session, loading, refresh, login, logout, can, canRepo,
      isAnonymous: !session || session.anonymous,
      isLocalUser: !!session && !session.anonymous && (session.via === 'session' || session.via === 'basic' || session.via === 'local'),
    }
  }, [methods, session, loading, refresh, login, logout])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useAuth() {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth outside AuthProvider')
  return v
}
