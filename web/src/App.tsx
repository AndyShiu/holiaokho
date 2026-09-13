import { Suspense, lazy, type ReactNode } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { useAuth } from './auth/AuthContext'
import AppShell from './layout/AppShell'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import ChangePassword from './pages/ChangePassword'

const Dashboard = lazy(() => import('./pages/Dashboard'))
const Browse = lazy(() => import('./pages/Browse'))
const Search = lazy(() => import('./pages/Search'))
const Repositories = lazy(() => import('./pages/admin/Repositories'))
const RepoWizard = lazy(() => import('./pages/admin/RepoWizard'))
const RepoDetail = lazy(() => import('./pages/admin/RepoDetail'))
const Storages = lazy(() => import('./pages/admin/Storages'))
const Users = lazy(() => import('./pages/admin/Users'))
const Roles = lazy(() => import('./pages/admin/Roles'))
const Selectors = lazy(() => import('./pages/admin/Selectors'))
const AuthSettings = lazy(() => import('./pages/admin/AuthSettings'))
const Tokens = lazy(() => import('./pages/Tokens'))
const Tasks = lazy(() => import('./pages/admin/Tasks'))
const Cleanup = lazy(() => import('./pages/admin/Cleanup'))
const RoutingRules = lazy(() => import('./pages/admin/RoutingRules'))
const Backup = lazy(() => import('./pages/admin/Backup'))
const Webhooks = lazy(() => import('./pages/admin/Webhooks'))
const Email = lazy(() => import('./pages/admin/Email'))
const System = lazy(() => import('./pages/system/System'))

function Center({ children }: { children: ReactNode }) {
  return <div style={{ minHeight: '60vh', display: 'grid', placeItems: 'center' }}>{children}</div>
}

// An account whose password somebody else chose may not go anywhere except
// the password form. The server enforces this too — this only saves the user
// from walking into a wall of 403s.
function mustChangePassword(session: { mustChangePassword?: boolean } | null, pathname: string) {
  return !!session?.mustChangePassword && pathname !== '/change-password'
}

function Guard({ children, target, action = 'read', authed }: { children: ReactNode; target?: string; action?: string; authed?: boolean }) {
  const { loading, session, can, methods } = useAuth()
  const loc = useLocation()
  if (loading) return <Center><Spin /></Center>
  if (mustChangePassword(session, loc.pathname)) return <Navigate to="/change-password?forced=1" replace />
  const anon = !session || session.anonymous
  if ((authed || target) && anon) {
    if (target && methods?.anonymous && can(target, action)) return <>{children}</>
    return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />
  }
  if (target && !can(target, action)) return <Navigate to="/browse" replace />
  return <>{children}</>
}

function ShellGuard() {
  const { loading, session, methods } = useAuth()
  const loc = useLocation()
  if (loading) return <Center><Spin /></Center>
  if (mustChangePassword(session, loc.pathname)) return <Navigate to="/change-password?forced=1" replace />
  const anon = !session || session.anonymous
  if (anon && methods && !methods.anonymous) return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />
  return <AppShell />
}

export default function App() {
  return (
    <BrowserRouter basename="/ui">
      <Suspense fallback={<Center><Spin /></Center>}>
        <ErrorBoundary>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/change-password" element={<Guard authed><ChangePassword /></Guard>} />
          <Route element={<ShellGuard />}>
            <Route index element={<Guard authed><Dashboard /></Guard>} />
            <Route path="/browse" element={<Browse />} />
            <Route path="/browse/:repo/*" element={<Browse />} />
            <Route path="/search" element={<Guard target="app:search"><Search /></Guard>} />
            <Route path="/admin/repositories" element={<Guard target="app:repositories"><Repositories /></Guard>} />
            <Route path="/admin/repositories/new" element={<Guard target="app:repositories" action="write"><RepoWizard /></Guard>} />
            <Route path="/admin/repositories/:name" element={<Guard target="app:repositories"><RepoDetail /></Guard>} />
            <Route path="/admin/repositories/:name/:tab" element={<Guard target="app:repositories"><RepoDetail /></Guard>} />
            <Route path="/admin/storages" element={<Guard target="app:storages"><Storages /></Guard>} />
            <Route path="/admin/users" element={<Guard target="app:users"><Users /></Guard>} />
            <Route path="/admin/roles" element={<Guard target="app:roles"><Roles /></Guard>} />
            <Route path="/admin/content-selectors" element={<Guard target="app:roles"><Selectors /></Guard>} />
            <Route path="/admin/auth" element={<Guard target="app:system"><AuthSettings /></Guard>} />
            <Route path="/me/tokens" element={<Guard authed><Tokens /></Guard>} />
            <Route path="/admin/tasks" element={<Guard target="app:tasks"><Tasks /></Guard>} />
            <Route path="/admin/cleanup" element={<Guard target="app:repositories"><Cleanup /></Guard>} />
            <Route path="/admin/routing-rules" element={<Guard target="app:repositories"><RoutingRules /></Guard>} />
            <Route path="/admin/backup" element={<Guard target="app:system" action="admin"><Backup /></Guard>} />
            <Route path="/admin/webhooks" element={<Guard target="app:system"><Webhooks /></Guard>} />
            <Route path="/admin/email" element={<Guard target="app:system"><Email /></Guard>} />
            <Route path="/admin/system" element={<Navigate to="/admin/system/health" replace />} />
            <Route path="/admin/system/:tab" element={<Guard target="app:system"><System /></Guard>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
        </ErrorBoundary>
      </Suspense>
    </BrowserRouter>
  )
}
