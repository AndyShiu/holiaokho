import { useEffect, useMemo, useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Badge, Button, Dropdown, Input, Layout, Menu, Tooltip, type MenuProps } from 'antd'
import {
  AppstoreOutlined, BellOutlined, ClockCircleOutlined, DatabaseOutlined, DeleteOutlined, FileSearchOutlined, FolderOpenOutlined, GlobalOutlined, HddOutlined,
  KeyOutlined, LogoutOutlined, MailOutlined, MoonOutlined, SafetyCertificateOutlined, SearchOutlined, SettingOutlined, SunOutlined, TeamOutlined, ToolOutlined,
  UserOutlined, ApiOutlined, BranchesOutlined, FilterOutlined, SaveOutlined, DashboardOutlined, LockOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/auth/AuthContext'
import { useTheme } from '@/theme/ThemeContext'
import { LANGS, setLanguage } from '@/i18n'
import { LogoMark, Wordmark } from '@/components/Logo'
import { get } from '@/api/client'
import type { Health, Status, Task } from '@/api/types'

const { Sider, Header, Content } = Layout

export function LangSwitch({ compact }: { compact?: boolean }) {
  const { i18n } = useTranslation()
  const cur = LANGS.find((l) => l.code === i18n.language) ?? LANGS[0]
  return (
    <Dropdown menu={{ items: LANGS.map((l) => ({ key: l.code, label: l.label, onClick: () => setLanguage(l.code) })), selectedKeys: [cur.code] }} trigger={['click']}>
      <Button type="text" size="small" icon={<GlobalOutlined />}>{compact ? cur.short : cur.label}</Button>
    </Dropdown>
  )
}

export function ThemeSwitch() {
  const { mode, setMode } = useTheme()
  const { t } = useTranslation()
  return (
    <Tooltip title={mode === 'dark' ? t('theme.light', 'Light theme') : t('theme.dark', 'Dark theme')}>
      <Button type="text" size="small" icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />} onClick={() => setMode(mode === 'dark' ? 'light' : 'dark')} />
    </Tooltip>
  )
}

function Notifications() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const navigate = useNavigate()
  const enabled = can('app:status', 'read') && can('app:tasks', 'read')
  const health = useQuery({ queryKey: ['health'], queryFn: () => get<Health>('status/check'), refetchInterval: 60000, enabled })
  const tasks = useQuery({ queryKey: ['tasks'], queryFn: () => get<Task[]>('tasks'), refetchInterval: 60000, enabled })
  if (!enabled) return null
  const items: MenuProps['items'] = []
  Object.entries(health.data?.checks ?? {}).forEach(([k, c]) => {
    if (!c.healthy) items.push({ key: `h:${k}`, label: <span><b>{k}</b> — {c.message ?? t('health.unhealthy', 'unhealthy')}</span>, onClick: () => navigate(k === 'default_admin_password' ? '/change-password' : '/admin/system/health') })
    else if (c.message) items.push({ key: `w:${k}`, label: <span><b>{k}</b> — {c.message}</span>, onClick: () => navigate('/admin/storages') })
  })
  ;(tasks.data ?? []).filter((x) => x.lastStatus === 'failed').forEach((x) => items.push({ key: `t:${x.name}`, label: <span>{t('tasks.failedNotice', 'Task {{name}} failed', { name: x.name })}</span>, onClick: () => navigate('/admin/tasks') }))
  if (!items.length) items.push({ key: 'none', label: <span style={{ color: 'var(--hlk-text-tertiary)' }}>{t('notify.none', 'Nothing needs attention')}</span>, disabled: true })
  return (
    <Dropdown menu={{ items }} trigger={['click']} placement="bottomRight">
      <Badge dot={items.some((i) => i && i.key !== 'none')} offset={[-4, 4]} color="var(--hlk-error)">
        <Button type="text" size="small" icon={<BellOutlined />} />
      </Badge>
    </Dropdown>
  )
}

export default function AppShell() {
  const { t } = useTranslation()
  const { session, can, logout, isAnonymous, isLocalUser } = useAuth()
  const loc = useLocation()
  const navigate = useNavigate()
  const [collapsed, setCollapsed] = useState(() => window.innerWidth < 1024)
  const [q, setQ] = useState('')
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status'), staleTime: 300000 })

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === '/' && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement)) {
        e.preventDefault()
        document.getElementById('hlk-global-search')?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const items = useMemo<MenuProps['items']>(() => {
    const it: MenuProps['items'] = []
    if (!isAnonymous) it.push({ key: '/', icon: <DashboardOutlined />, label: <Link to="/">{t('nav.dashboard', 'Dashboard')}</Link> })
    it.push({ key: '/browse', icon: <FolderOpenOutlined />, label: <Link to="/browse">{t('nav.browse', 'Browse')}</Link> })
    if (can('app:search', 'read')) it.push({ key: '/search', icon: <SearchOutlined />, label: <Link to="/search">{t('nav.search', 'Search')}</Link> })
    const manage: MenuProps['items'] = []
    if (can('app:repositories', 'read')) manage.push({ key: '/admin/repositories', icon: <AppstoreOutlined />, label: <Link to="/admin/repositories">{t('nav.repositories', 'Repositories')}</Link> })
    if (can('app:storages', 'read')) manage.push({ key: '/admin/storages', icon: <HddOutlined />, label: <Link to="/admin/storages">{t('nav.storages', 'Storages')}</Link> })
    if (manage.length) it.push({ type: 'group', key: 'g-manage', label: t('nav.group.manage', 'Manage'), children: manage })
    const sec: MenuProps['items'] = []
    if (can('app:users', 'read')) sec.push({ key: '/admin/users', icon: <UserOutlined />, label: <Link to="/admin/users">{t('nav.users', 'Users')}</Link> })
    if (can('app:roles', 'read')) {
      sec.push({ key: '/admin/roles', icon: <TeamOutlined />, label: <Link to="/admin/roles">{t('nav.roles', 'Roles')}</Link> })
      sec.push({ key: '/admin/content-selectors', icon: <FilterOutlined />, label: <Link to="/admin/content-selectors">{t('nav.selectors', 'Content Selectors')}</Link> })
    }
    if (can('app:system', 'read')) sec.push({ key: '/admin/auth', icon: <SafetyCertificateOutlined />, label: <Link to="/admin/auth">{t('nav.auth', 'Auth Settings')}</Link> })
    if (!isAnonymous) sec.push({ key: '/me/tokens', icon: <KeyOutlined />, label: <Link to="/me/tokens">{t('nav.tokens', 'My Tokens')}</Link> })
    if (sec.length) it.push({ type: 'group', key: 'g-sec', label: t('nav.group.security', 'Security'), children: sec })
    const maint: MenuProps['items'] = []
    if (can('app:tasks', 'read')) maint.push({ key: '/admin/tasks', icon: <ClockCircleOutlined />, label: <Link to="/admin/tasks">{t('nav.tasks', 'Tasks')}</Link> })
    if (can('app:repositories', 'read')) {
      maint.push({ key: '/admin/cleanup', icon: <DeleteOutlined />, label: <Link to="/admin/cleanup">{t('nav.cleanup', 'Cleanup Policies')}</Link> })
      maint.push({ key: '/admin/routing-rules', icon: <BranchesOutlined />, label: <Link to="/admin/routing-rules">{t('nav.routing', 'Routing Rules')}</Link> })
    }
    if (can('app:system', 'admin')) maint.push({ key: '/admin/backup', icon: <SaveOutlined />, label: <Link to="/admin/backup">{t('nav.backup', 'Backup / Restore')}</Link> })
    if (maint.length) it.push({ type: 'group', key: 'g-maint', label: t('nav.group.maintenance', 'Maintenance'), children: maint })
    const more: MenuProps['items'] = []
    if (can('app:system', 'read')) {
      more.push({ key: '/admin/webhooks', icon: <ApiOutlined />, label: <Link to="/admin/webhooks">{t('nav.webhooks', 'Webhooks')}</Link> })
      more.push({ key: '/admin/email', icon: <MailOutlined />, label: <Link to="/admin/email">{t('nav.email', 'Email')}</Link> })
      more.push({ key: '/admin/system', icon: <ToolOutlined />, label: <Link to="/admin/system/health">{t('nav.system', 'System')}</Link> })
    }
    if (more.length) it.push({ type: 'group', key: 'g-more', label: t('nav.group.more', 'More'), children: more })
    return it
  }, [can, isAnonymous, t])

  const selected = useMemo(() => {
    const p = loc.pathname
    if (p.startsWith('/admin/system')) return ['/admin/system']
    if (p.startsWith('/admin/repositories')) return ['/admin/repositories']
    if (p.startsWith('/browse')) return ['/browse']
    const keys: string[] = []
    const walk = (arr: any[]) => arr.forEach((i) => { if (i?.children) walk(i.children); else if (i?.key) keys.push(i.key) })
    walk(items ?? [])
    const match = keys.filter((k) => p === k || (k !== '/' && p.startsWith(k))).sort((a, b) => b.length - a.length)[0]
    return match ? [match] : []
  }, [loc.pathname, items])

  const userMenu: MenuProps['items'] = isAnonymous
    ? [{ key: 'login', icon: <LockOutlined />, label: t('nav.login', 'Log in'), onClick: () => navigate(`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`) }]
    : [
        { key: 'tokens', icon: <KeyOutlined />, label: t('nav.tokens', 'My Tokens'), onClick: () => navigate('/me/tokens') },
        ...(isLocalUser ? [{ key: 'pw', icon: <SettingOutlined />, label: t('nav.changePassword', 'Change password'), onClick: () => navigate('/change-password') }] : []),
        { type: 'divider' as const },
        { key: 'logout', icon: <LogoutOutlined />, label: t('nav.logout', 'Log out'), onClick: async () => { await logout(); navigate('/login') } },
      ]

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        width={232} collapsedWidth={56} collapsible collapsed={collapsed} trigger={null}
        style={{ borderRight: '1px solid var(--hlk-border)', position: 'sticky', top: 0, height: '100vh', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
      >
        <div style={{ padding: collapsed ? '14px 12px' : '14px 12px 8px', display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', flex: 'none' }} onClick={() => setCollapsed(!collapsed)}>
          <LogoMark size={22} ink={'var(--hlk-text)'} />
          {!collapsed && (
            <>
              <Wordmark size={15} />
              <span className="hlk-mono" style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{status.data?.version ? `v${status.data.version}` : ''}</span>
            </>
          )}
        </div>
        <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', overflowX: 'hidden' }}>
          <Menu mode="inline" items={items} selectedKeys={selected} style={{ background: 'transparent', padding: '0 12px 8px', fontSize: 13 }} inlineIndent={10} />
        </div>
        <div style={{ flex: 'none', padding: 12, borderTop: '1px solid var(--hlk-border)' }}>
          <Dropdown menu={{ items: userMenu }} trigger={['click']} placement="topLeft">
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer', padding: 4 }}>
              <Avatar size={26} style={{ background: 'var(--hlk-ink)', color: '#F3EFE7', fontSize: 12, flex: 'none' }} icon={isAnonymous ? <UserOutlined /> : undefined}>
                {!isAnonymous ? session?.username.slice(0, 2).toUpperCase() : undefined}
              </Avatar>
              {!collapsed && (
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 13, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{isAnonymous ? t('nav.anonymous', 'Anonymous') : session?.username}</div>
                  <div className="hlk-mono" style={{ fontSize: 10, color: 'var(--hlk-text-tertiary)' }}>{isAnonymous ? t('nav.clickToLogin', 'click to log in') : `via ${session?.via}`}</div>
                </div>
              )}
            </div>
          </Dropdown>
        </div>
      </Sider>
      <Layout>
        <Header style={{ display: 'flex', alignItems: 'center', gap: 12, borderBottom: '1px solid var(--hlk-border)', position: 'sticky', top: 0, zIndex: 10, lineHeight: 'normal' }}>
          <Input
            id="hlk-global-search"
            prefix={<SearchOutlined style={{ color: 'var(--hlk-text-tertiary)' }} />}
            suffix={<kbd style={{ fontSize: 10, color: 'var(--hlk-text-tertiary)', border: '1px solid var(--hlk-border)', borderRadius: 3, padding: '0 4px' }}>/</kbd>}
            placeholder={t('search.placeholder', 'Search packages… e.g. gson, @babel/core, library/alpine')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onPressEnter={() => { navigate(`/search?q=${encodeURIComponent(q)}`); setQ('') }}
            style={{ width: 380, maxWidth: '50vw', height: 32 }}
            size="small"
          />
          <div style={{ flex: 1 }} />
          <LangSwitch compact />
          <ThemeSwitch />
          <Notifications />
        </Header>
        <Content style={{ padding: 24, minWidth: 0 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
