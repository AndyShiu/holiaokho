import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Alert, App, Button, Checkbox, Form, Input, InputNumber, Select, Skeleton, Switch, Tag } from 'antd'
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, HolderOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post, put } from '@/api/client'
import type { AuthSettings as AS, LDAPConfig, OIDCConfig, PasswordPolicy, Role, RutConfig } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { PageHeader, SecretHint, useErrorText } from '@/components/Common'
import { Copyable } from '@/components/Copyable'
import { TestPanel } from './Selectors'

type SectionKey = 'realms' | 'roles' | 'password' | 'ldap' | 'oidc' | 'rut'

// The backend omits empty arrays/objects as null; fill them so the form never crashes.
function normalize(s: AS): AS {
  const d: AS = JSON.parse(JSON.stringify(s))
  d.realms = d.realms ?? ['local']
  d.defaultRoles = d.defaultRoles ?? []
  d.ldap = { ...d.ldap, roleMapping: d.ldap?.roleMapping ?? {}, defaultRoles: d.ldap?.defaultRoles ?? [], scopes: (d.ldap as any)?.scopes } as any
  d.oidc = { ...d.oidc, roleMapping: d.oidc?.roleMapping ?? {}, defaultRoles: d.oidc?.defaultRoles ?? [], scopes: d.oidc?.scopes ?? [] }
  d.rut = { ...d.rut, defaultRoles: d.rut?.defaultRoles ?? [], trustedProxies: d.rut?.trustedProxies ?? [] }
  return d
}

// Stable component (defined at module level so inputs keep focus across renders).
function F({ label, children, span }: { label: string; children: ReactNode; span?: boolean }) {
  return <div style={{ marginBottom: 12, gridColumn: span ? '1 / -1' : undefined }}><div style={{ fontSize: 12, fontWeight: 500, marginBottom: 4 }}>{label}</div>{children}</div>
}

function Card({ id, title, summary, dirty, children, onSave, saving, disabled }: { id: string; title: string; summary: ReactNode; dirty: boolean; children: ReactNode; onSave: () => void; saving: boolean; disabled?: boolean }) {
  const { t } = useTranslation()
  return (
    <div id={id} className="hlk-card" style={{ marginBottom: 16, scrollMarginTop: 80 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <div style={{ fontWeight: 600, fontSize: 15, minWidth: 150 }}>{title}</div>
        <div style={{ flex: 1, fontSize: 12, color: 'var(--hlk-text-secondary)' }} className="hlk-mono">{summary}</div>
        {dirty && <span className="hlk-unsaved">● {t('common.unsaved', 'Unsaved changes')}</span>}
        <Button type="primary" size="small" disabled={!dirty || disabled} loading={saving} onClick={onSave}>{t('common.save', 'Save')}</Button>
      </div>
      {children}
    </div>
  )
}

function MappingTable({ value, onChange, roles, leftLabel }: { value: Record<string, string>; onChange: (v: Record<string, string>) => void; roles: Role[]; leftLabel: string }) {
  const { t } = useTranslation()
  const roleLabel = t('auth.roleColumn', 'ROLE')
  const rows = Object.entries(value)
  const [newKey, setNewKey] = useState('')
  return (
    <div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 20px 1fr 32px', gap: 8, fontSize: 10, letterSpacing: '.06em', color: 'var(--hlk-text-tertiary)', marginBottom: 4 }} className="hlk-mono"><span>{leftLabel}</span><span /><span>{roleLabel}</span><span /></div>
      {rows.map(([g, r]) => (
        <div key={g} style={{ display: 'grid', gridTemplateColumns: '1fr 20px 1fr 32px', gap: 8, alignItems: 'center', marginBottom: 6 }}>
          <span className="hlk-mono" style={{ fontSize: 12 }}>{g}</span>
          <span style={{ textAlign: 'center', color: 'var(--hlk-text-tertiary)' }}>→</span>
          <Select size="small" value={r} onChange={(x) => onChange({ ...value, [g]: x })} options={roles.map((x) => ({ value: x.id }))} />
          <Button size="small" type="text" icon={<DeleteOutlined />} onClick={() => { const v = { ...value }; delete v[g]; onChange(v) }} />
        </div>
      ))}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 20px 1fr 32px', gap: 8, alignItems: 'center' }}>
        <Input size="small" className="hlk-mono" placeholder={leftLabel} value={newKey} onChange={(e) => setNewKey(e.target.value)} onPressEnter={() => { if (newKey) { onChange({ ...value, [newKey]: roles[0]?.id ?? 'developer' }); setNewKey('') } }} />
        <span />
        <Button size="small" type="link" icon={<PlusOutlined />} disabled={!newKey} onClick={() => { onChange({ ...value, [newKey]: roles[0]?.id ?? 'developer' }); setNewKey('') }} style={{ justifySelf: 'start', paddingLeft: 0 }}>{t('auth.addMapping', 'Add mapping')}</Button>
      </div>
    </div>
  )
}

export default function AuthSettings() {
  const { t } = useTranslation()
  const { can, methods, refreshMethods: refreshAuth } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const q = useQuery({ queryKey: ['auth-settings'], queryFn: () => get<AS>('auth/settings') })
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => get<Role[]>('roles') })
  const cfg = useQuery({ queryKey: ['system-config'], queryFn: () => get<any>('system/config') })
  const [draft, setDraft] = useState<AS | null>(null)
  const [dirty, setDirty] = useState<Set<SectionKey>>(new Set())
  const [saving, setSaving] = useState<SectionKey | null>(null)
  const [active, setActive] = useState<SectionKey>('realms')
  const [dragFrom, setDragFrom] = useState<number | null>(null)
  const [dragOver, setDragOver] = useState<number | null>(null)
  const [ldapTest, setLdapTest] = useState({ username: '', password: '' })
  const [ldapResult, setLdapResult] = useState<{ ok: boolean; ms?: number; user?: any; error?: string } | null>(null)
  const canWrite = can('app:system', 'write')
  useEffect(() => { if (q.data && !draft) setDraft(normalize(q.data)) }, [q.data, draft])
  const roleList = roles.data ?? []
  const mark = (k: SectionKey, patch: Partial<AS>) => { setDraft((d) => (d ? { ...d, ...patch } : d)); setDirty((s) => new Set(s).add(k)) }
  const setL = (p: Partial<LDAPConfig>) => mark('ldap', { ldap: { ...draft!.ldap, ...p } })
  const setO = (p: Partial<OIDCConfig>) => mark('oidc', { oidc: { ...draft!.oidc, ...p } })
  const setR = (p: Partial<RutConfig>) => mark('rut', { rut: { ...draft!.rut, ...p } })
  const setP = (p: Partial<PasswordPolicy>) => mark('password', { password: { ...draft!.password, ...p } })

  // Scroll spy. An IntersectionObserver is used rather than a scroll
  // listener: the page scrolls the document element and scroll events do not
  // reach window here, so a listener would never fire.
  useEffect(() => {
    if (!draft) return
    const keys: SectionKey[] = ['realms', 'roles', 'password', 'ldap', 'oidc', 'rut']
    const els = keys.map((k) => document.getElementById(`auth-${k}`)).filter((e): e is HTMLElement => !!e)
    if (!els.length) return
    const visible = new Map<string, number>()
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.set(e.target.id, e.boundingClientRect.top)
          else visible.delete(e.target.id)
        }
        // The highest section still crossing the top band wins.
        let best: SectionKey | null = null
        let bestTop = Infinity
        for (const [id, top] of visible) {
          if (top < bestTop) {
            bestTop = top
            best = id.replace('auth-', '') as SectionKey
          }
        }
        if (best) setActive(best)
      },
      // Only the band just below the sticky header counts as "current".
      { rootMargin: '-80px 0px -70% 0px', threshold: 0 },
    )
    els.forEach((e) => io.observe(e))
    // The last section can never reach the top band, so the end of the page
    // coming into view activates it instead.
    const end = document.getElementById('auth-end')
    const endIo = end
      ? new IntersectionObserver((entries) => { if (entries.some((e) => e.isIntersecting)) setActive(keys[keys.length - 1]) }, { threshold: 0 })
      : null
    if (end && endIo) endIo.observe(end)
    return () => {
      io.disconnect()
      endIo?.disconnect()
    }
  }, [draft])

  const save = async (k: SectionKey) => {
    if (!draft || !q.data) return
    setSaving(k)
    try {
      // Each card saves only its own block on top of the last-loaded settings.
      const base: AS = JSON.parse(JSON.stringify(q.data))
      const merged: AS = { ...base }
      if (k === 'realms') {
        merged.realms = draft.realms
        merged.anonymous = draft.anonymous ?? methods?.anonymous ?? false
      }
      if (k === 'roles') merged.defaultRoles = draft.defaultRoles
      if (k === 'password') merged.password = draft.password
      if (k === 'ldap') merged.ldap = draft.ldap
      if (k === 'oidc') merged.oidc = draft.oidc
      if (k === 'rut') merged.rut = draft.rut
      await put('auth/settings', merged)
      await qc.invalidateQueries({ queryKey: ['auth-settings'] })
      const fresh = await qc.fetchQuery({ queryKey: ['auth-settings'], queryFn: () => get<AS>('auth/settings') })
      setDraft((d) => (d ? normalize({ ...d, [k === 'roles' ? 'defaultRoles' : k]: (fresh as any)[k === 'roles' ? 'defaultRoles' : k] } as AS) : d))
      setDirty((s) => { const n = new Set(s); n.delete(k); return n })
      if (k === 'realms') refreshAuth()
      message.success(t('auth.saved', 'Saved — takes effect within 30 seconds'))
    } catch (e) {
      message.error(errText(e))
    } finally {
      setSaving(null)
    }
  }

  const testLdap = async () => {
    setLdapResult(null)
    const started = Date.now()
    try {
      const r = await post<{ connected: boolean; user?: any }>('auth/ldap/test', { config: draft!.ldap, ...ldapTest })
      setLdapResult({ ok: true, ms: Date.now() - started, user: r.user })
    } catch (e) {
      setLdapResult({ ok: false, error: errText(e) })
    }
  }

  const sections = useMemo(() => draft ? [
    { key: 'realms', label: t('auth.realms', 'Realms & anonymous'), sum: `${draft.realms.join(' · ')}${(draft.anonymous ?? methods?.anonymous) ? ' · anon' : ''}` },
    { key: 'roles', label: t('auth.defaultRoles', 'Default roles'), sum: draft.defaultRoles.join(', ') || '—' },
    { key: 'password', label: t('auth.password', 'Password policy'), sum: `${draft.password.minLength}+ · ${draft.password.requireUpper ? 'A' : ''}${draft.password.requireLower ? 'a' : ''}${draft.password.requireDigit ? '1' : ''}${draft.password.requireSymbol ? '#' : ''}` },
    { key: 'ldap', label: 'LDAP', sum: draft.ldap.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off') },
    { key: 'oidc', label: 'OIDC', sum: draft.oidc.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off') },
    { key: 'rut', label: 'Rut Auth', sum: draft.rut.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off') },
  ] as { key: SectionKey; label: string; sum: string }[] : [], [draft, t])

  if (!draft) return <Skeleton active />
  const anon = draft.anonymous ?? methods?.anonymous ?? false
  const realms = draft.realms
  const moveRealm = (i: number, d: number) => { const r = [...realms]; const j = i + d; if (j < 0 || j >= r.length) return; [r[i], r[j]] = [r[j], r[i]]; mark('realms', { realms: r }) }
  const trusted = cfg.data?.server?.trusted_proxies ?? cfg.data?.Server?.TrustedProxies ?? []
  const redirect = `${window.location.origin}/api/v1/auth/oidc/callback`
  const grid2: React.CSSProperties = { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '4px 16px', alignItems: 'end' }

  return (
    <>
      <PageHeader title={t('nav.auth', 'Auth Settings')} sub={<span style={{ color: 'var(--hlk-text-tertiary)' }}>{t('nav.group.security', 'Security')} › {t('nav.auth', 'Auth Settings')}</span>} />
      <div className="hlk-auth-grid">
        <div style={{ position: 'sticky', top: 76 }}>
          {sections.map((s) => (
            <a
              key={s.key}
              href={`#auth-${s.key}`}
              className={`hlk-anchor${active === s.key ? ' active' : ''}`}
              onClick={(e) => {
                e.preventDefault()
                setActive(s.key)
                document.getElementById(`auth-${s.key}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
              }}
            >
              <span>{s.label}</span>
              <span className="hlk-mono" style={{ fontSize: 11, color: dirty.has(s.key) ? 'var(--hlk-unsaved)' : undefined }}>{s.sum}</span>
            </a>
          ))}
        </div>
        <div>
          <Card id="auth-realms" title={sections[0].label} summary={sections[0].sum} dirty={dirty.has('realms')} onSave={() => save('realms')} saving={saving === 'realms'} disabled={!canWrite}>
            <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginBottom: 10 }}>{t('auth.realmsHint2', 'On login the realms are tried from top to bottom; the first one that accepts the password wins. Drag to reorder.')}</div>
            <div style={{ maxWidth: 520 }}>
              {realms.map((r, i) => (
                <div
                  key={r}
                  className={`hlk-realm${dragOver === i ? ' over' : ''}`}
                  draggable={canWrite}
                  onDragStart={() => setDragFrom(i)}
                  onDragOver={(e) => { e.preventDefault(); setDragOver(i) }}
                  onDragLeave={() => setDragOver((v) => (v === i ? null : v))}
                  onDragEnd={() => { setDragFrom(null); setDragOver(null) }}
                  onDrop={(e) => {
                    e.preventDefault()
                    if (dragFrom !== null && dragFrom !== i) {
                      const next = [...realms]
                      const [moved] = next.splice(dragFrom, 1)
                      next.splice(i, 0, moved)
                      mark('realms', { realms: next })
                    }
                    setDragFrom(null); setDragOver(null)
                  }}
                >
                  <HolderOutlined className="hlk-realm-grip" />
                  <span className="hlk-realm-no">{i + 1}</span>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div className="hlk-mono" style={{ fontSize: 13, fontWeight: 500 }}>{r}</div>
                    <div style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{t(`auth.realmDesc.${r}`, r === 'local' ? 'Local accounts stored in Holiaokho' : 'Accounts from the LDAP directory')}</div>
                  </div>
                  <Button type="text" size="small" disabled={!canWrite || i === 0} icon={<ArrowUpOutlined />} onClick={() => moveRealm(i, -1)} />
                  <Button type="text" size="small" disabled={!canWrite || i === realms.length - 1} icon={<ArrowDownOutlined />} onClick={() => moveRealm(i, 1)} />
                  <Button type="text" size="small" danger disabled={!canWrite || r === 'local'} icon={<DeleteOutlined />} onClick={() => mark('realms', { realms: realms.filter((x) => x !== r) })} />
                </div>
              ))}
              {!realms.includes('ldap') && (
                <Button size="small" type="dashed" block icon={<PlusOutlined />} disabled={!canWrite} onClick={() => mark('realms', { realms: [...realms, 'ldap'] })} style={{ marginTop: 6 }}>
                  {t('auth.addLdapRealm2', 'Add the LDAP realm')}
                </Button>
              )}
            </div>
            {draft.ldap.enabled && !realms.includes('ldap') && <Alert type="warning" showIcon style={{ marginTop: 10 }} message={t('auth.ldapNotInRealms', 'LDAP is enabled but not in the realm order, so it will not be used for login.')} />}
            <div style={{ marginTop: 16, paddingTop: 14, borderTop: '1px solid var(--hlk-row)' }}>
              <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
                <Switch checked={anon} disabled={!canWrite} onChange={(v) => mark('realms', { anonymous: v })} />
                <span style={{ fontSize: 13 }}>{t('auth.anonymous', 'Anonymous access')}</span>
                <Tag color={anon ? 'success' : 'default'} style={{ margin: 0 }}>{anon ? t('common.enabled', 'enabled') : t('common.off', 'off')}</Tag>
                <Link to="/admin/roles" style={{ fontSize: 12 }}>{t('auth.viewAnonRole', 'View the anonymous role →')}</Link>
              </div>
              <div style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginTop: 6 }}>{t('auth.anonymousHint', 'Lets clients read without logging in. What they may do comes from the anonymous role.')}</div>
            </div>
          </Card>

          <Card id="auth-roles" title={sections[1].label} summary={sections[1].sum} dirty={dirty.has('roles')} onSave={() => save('roles')} saving={saving === 'roles'} disabled={!canWrite}>
            <Select mode="multiple" style={{ width: '100%', maxWidth: 480 }} value={draft.defaultRoles} onChange={(v) => mark('roles', { defaultRoles: v })} options={roleList.map((r) => ({ value: r.id }))} placeholder={t('common.none', 'None')} />
            <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginTop: 8 }}>{t('auth.defaultRolesHint', 'Granted to every authenticated user in addition to their own roles.')}</div>
          </Card>

          <Card id="auth-password" title={sections[2].label} summary={sections[2].sum} dirty={dirty.has('password')} onSave={() => save('password')} saving={saving === 'password'} disabled={!canWrite}>
            <div className="hlk-side-grid">
              <div>
                <div style={grid2}>
                  <F label={t('auth.minLength', 'Minimum length')}><InputNumber min={8} max={128} value={draft.password.minLength} onChange={(v) => setP({ minLength: v ?? 12 })} /></F>
                  <F label={t('auth.maxLength', 'Maximum length')}><InputNumber min={8} max={1024} value={draft.password.maxLength} onChange={(v) => setP({ maxLength: v ?? 128 })} /></F>
                </div>
                <div style={{ display: 'grid', gap: 6 }}>
                  <Checkbox checked={draft.password.requireUpper} onChange={(e) => setP({ requireUpper: e.target.checked })}>{t('password.rule.upper', 'An uppercase letter')}</Checkbox>
                  <Checkbox checked={draft.password.requireLower} onChange={(e) => setP({ requireLower: e.target.checked })}>{t('password.rule.lower', 'A lowercase letter')}</Checkbox>
                  <Checkbox checked={draft.password.requireDigit} onChange={(e) => setP({ requireDigit: e.target.checked })}>{t('password.rule.digit', 'A digit')}</Checkbox>
                  <Checkbox checked={draft.password.requireSymbol} onChange={(e) => setP({ requireSymbol: e.target.checked })}>{t('password.rule.symbol', 'A symbol')}</Checkbox>
                  <Checkbox checked={draft.password.disallowUsername} onChange={(e) => setP({ disallowUsername: e.target.checked })}>{t('password.rule.username', 'Does not contain the username')}</Checkbox>
                  <Checkbox checked={draft.password.disallowCommon} onChange={(e) => setP({ disallowCommon: e.target.checked })}>{t('auth.disallowCommon', 'Reject common passwords (built-in list)')}</Checkbox>
                </div>
              </div>
              <TestPanel title={t('auth.pwExample', 'A valid password looks like')}>
                <code className="hlk-mono" style={{ fontSize: 13 }}>{'Correct-Horse-9-Battery'.slice(0, Math.max(draft.password.minLength, 12))}{draft.password.requireSymbol ? '!' : ''}</code>
                <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginTop: 8 }}>{t('auth.pwHint', 'Applies to user creation, admin resets and self-service changes. Existing passwords are not re-checked.')}</div>
              </TestPanel>
            </div>
          </Card>

          <Card id="auth-ldap" title="LDAP" summary={sections[3].sum} dirty={dirty.has('ldap')} onSave={() => save('ldap')} saving={saving === 'ldap'} disabled={!canWrite || (draft.ldap.enabled && (!draft.ldap.url || !draft.ldap.userBaseDn))}>
            <div className="hlk-side-grid">
              <div>
                <div style={{ marginBottom: 14 }}><Switch checked={draft.ldap.enabled} onChange={(v) => setL({ enabled: v })} /> <span style={{ marginLeft: 8 }}>{draft.ldap.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off')}</span></div>
                <div style={grid2}>
                  <F label="URL"><Input className="hlk-mono" value={draft.ldap.url} onChange={(e) => setL({ url: e.target.value })} placeholder="ldaps://ldap.example.com:636" /></F>
                  <F label={t('auth.options', 'Options')}><span style={{ display: 'flex', gap: 12 }}><Checkbox checked={draft.ldap.startTls} onChange={(e) => setL({ startTls: e.target.checked })}>StartTLS</Checkbox><Checkbox checked={draft.ldap.insecureSkipVerify} onChange={(e) => setL({ insecureSkipVerify: e.target.checked })}>{t('auth.skipVerify', 'Skip TLS verify')}</Checkbox></span></F>
                  <F label="Bind DN"><Input className="hlk-mono" value={draft.ldap.bindDn} onChange={(e) => setL({ bindDn: e.target.value })} /></F>
                  <F label={t('auth.bindPassword', 'Bind password')}><Input.Password value={draft.ldap.bindPassword ?? ''} onChange={(e) => setL({ bindPassword: e.target.value })} autoComplete="new-password" placeholder="***" /><SecretHint /></F>
                  <F label={t('auth.userBaseDn', 'User base DN')}><Input className="hlk-mono" value={draft.ldap.userBaseDn} onChange={(e) => setL({ userBaseDn: e.target.value })} /></F>
                  <F label={t('auth.userFilter', 'User filter')}><Input className="hlk-mono" value={draft.ldap.userFilter} onChange={(e) => setL({ userFilter: e.target.value })} placeholder="(uid={username})" /></F>
                  <F label={t('auth.emailAttr', 'Email attribute')}><Input className="hlk-mono" value={draft.ldap.emailAttr} onChange={(e) => setL({ emailAttr: e.target.value })} /></F>
                  <F label={t('auth.displayNameAttr', 'Display name attribute')}><Input className="hlk-mono" value={draft.ldap.displayNameAttr} onChange={(e) => setL({ displayNameAttr: e.target.value })} /></F>
                  <F label={t('auth.groupBaseDn', 'Group base DN')}><Input className="hlk-mono" value={draft.ldap.groupBaseDn} onChange={(e) => setL({ groupBaseDn: e.target.value })} /></F>
                  <F label={t('auth.groupFilter', 'Group filter')}><Input className="hlk-mono" value={draft.ldap.groupFilter} onChange={(e) => setL({ groupFilter: e.target.value })} placeholder="(member={dn})" /></F>
                  <F label={t('auth.groupNameAttr', 'Group name attribute')}><Input className="hlk-mono" value={draft.ldap.groupNameAttr} onChange={(e) => setL({ groupNameAttr: e.target.value })} /></F>
                  <F label={t('auth.memberOfAttr', 'memberOf attribute')}><Input className="hlk-mono" value={draft.ldap.memberOfAttr} onChange={(e) => setL({ memberOfAttr: e.target.value })} /></F>
                  <F label={t('auth.timeout', 'Timeout (s)')}><InputNumber min={1} value={draft.ldap.timeoutSeconds} onChange={(v) => setL({ timeoutSeconds: v ?? 10 })} /></F>
                  <F label={t('auth.userSubtree', 'Search subtree')}><Checkbox checked={draft.ldap.userSubtree} onChange={(e) => setL({ userSubtree: e.target.checked })} /></F>
                </div>
                <F label={t('auth.roleMapping', 'Role mapping')} span><MappingTable value={draft.ldap.roleMapping ?? {}} onChange={(v) => setL({ roleMapping: v })} roles={roleList} leftLabel={t('auth.ldapGroup', 'LDAP GROUP')} /></F>
                <F label={t('auth.ldapDefaultRoles', 'Default roles for LDAP users')}><Select mode="multiple" style={{ width: '100%', maxWidth: 400 }} value={draft.ldap.defaultRoles ?? []} onChange={(v) => setL({ defaultRoles: v })} options={roleList.map((r) => ({ value: r.id }))} /></F>
              </div>
              <TestPanel title={t('auth.testLogin', 'Test connection / login')}>
                <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginBottom: 10 }}>{t('auth.testHint', 'Uses the unsaved form values above.')}</div>
                <Input size="small" placeholder={t('login.username', 'Username')} value={ldapTest.username} onChange={(e) => setLdapTest({ ...ldapTest, username: e.target.value })} style={{ marginBottom: 8 }} />
                <Input.Password size="small" placeholder={t('login.password', 'Password')} value={ldapTest.password} onChange={(e) => setLdapTest({ ...ldapTest, password: e.target.value })} style={{ marginBottom: 12 }} />
                <div style={{ display: 'flex', gap: 8 }}><Button size="small" onClick={() => { setLdapTest({ username: '', password: '' }); testLdap() }}>{t('auth.testConn', 'Test connection')}</Button><Button size="small" type="primary" disabled={!ldapTest.username} onClick={testLdap}>{t('auth.testLoginBtn', 'Test login')}</Button></div>
                {ldapResult && (
                  <div style={{ marginTop: 12 }}>
                    {ldapResult.ok ? (
                      <div style={{ background: 'var(--hlk-success-bg)', border: '1px solid var(--hlk-success)', borderRadius: 6, padding: '8px 10px', fontSize: 12 }}>
                        <div style={{ color: 'var(--hlk-success)', fontWeight: 500 }}>✓ {ldapResult.user ? t('auth.loginOk', 'Login succeeded') : t('auth.connOk', 'Connected')} · {ldapResult.ms} ms</div>
                        {ldapResult.user && Object.entries(ldapResult.user).map(([k, v]) => <div key={k} style={{ display: 'grid', gridTemplateColumns: '90px 1fr', gap: 6, marginTop: 4 }}><span style={{ color: 'var(--hlk-text-secondary)' }}>{k}</span><span className="hlk-mono" style={{ wordBreak: 'break-all' }}>{Array.isArray(v) ? v.join(', ') : String(v)}</span></div>)}
                      </div>
                    ) : <Alert type="error" showIcon message={ldapResult.error} />}
                  </div>
                )}
              </TestPanel>
            </div>
          </Card>

          <Card id="auth-oidc" title="OIDC" summary={sections[4].sum} dirty={dirty.has('oidc')} onSave={() => save('oidc')} saving={saving === 'oidc'} disabled={!canWrite || (draft.oidc.enabled && (!draft.oidc.issuer || !draft.oidc.clientId))}>
            <div style={{ marginBottom: 14 }}><Switch checked={draft.oidc.enabled} onChange={(v) => setO({ enabled: v })} /> <span style={{ marginLeft: 8 }}>{draft.oidc.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off')}</span></div>
            <div style={grid2}>
              <F label="Issuer"><Input className="hlk-mono" value={draft.oidc.issuer} onChange={(e) => setO({ issuer: e.target.value })} placeholder="https://accounts.example.com" /></F>
              <F label="Client ID"><Input className="hlk-mono" value={draft.oidc.clientId} onChange={(e) => setO({ clientId: e.target.value })} /></F>
              <F label="Client secret"><Input.Password value={draft.oidc.clientSecret ?? ''} onChange={(e) => setO({ clientSecret: e.target.value })} autoComplete="new-password" placeholder="***" /><SecretHint /></F>
              <F label="Scopes"><Select mode="tags" style={{ width: '100%' }} value={draft.oidc.scopes ?? []} onChange={(v) => setO({ scopes: v })} tokenSeparators={[' ', ',']} /></F>
              <F label={t('auth.usernameClaim', 'Username claim')}><Input className="hlk-mono" value={draft.oidc.usernameClaim} onChange={(e) => setO({ usernameClaim: e.target.value })} placeholder="preferred_username" /></F>
              <F label={t('auth.groupsClaim', 'Groups claim')}><Input className="hlk-mono" value={draft.oidc.groupsClaim} onChange={(e) => setO({ groupsClaim: e.target.value })} placeholder="groups" /></F>
              <F label={t('auth.redirect', 'Redirect URL (register at the IdP)')} span><Copyable text={redirect} style={{ fontSize: 12 }} /></F>
              <F label={t('auth.roleMapping', 'Role mapping')} span><MappingTable value={draft.oidc.roleMapping ?? {}} onChange={(v) => setO({ roleMapping: v })} roles={roleList} leftLabel={t('auth.groupClaim', 'GROUP CLAIM')} /></F>
              <F label={t('auth.oidcDefaultRoles', 'Default roles for OIDC users')}><Select mode="multiple" style={{ width: '100%' }} value={draft.oidc.defaultRoles ?? []} onChange={(v) => setO({ defaultRoles: v })} options={roleList.map((r) => ({ value: r.id }))} /></F>
              <F label={t('auth.options', 'Options')}><Checkbox checked={draft.oidc.insecureSkipIssuerVerify} onChange={(e) => setO({ insecureSkipIssuerVerify: e.target.checked })}>{t('auth.skipIssuer', 'Skip issuer verification')}</Checkbox></F>
            </div>
          </Card>

          <Card id="auth-rut" title="Rut Auth" summary={sections[5].sum} dirty={dirty.has('rut')} onSave={() => save('rut')} saving={saving === 'rut'} disabled={!canWrite}>
            <div style={{ fontSize: 13, color: 'var(--hlk-text-secondary)', marginBottom: 12, lineHeight: 1.7 }}>
              {t('auth.rutDesc', 'For setups where a reverse proxy or SSO gateway has already authenticated the user and passes the username in an HTTP header. Holiaokho then trusts that header and logs the user in without a password. If you are adding SSO from scratch, prefer OIDC above.')}
            </div>
            <Alert type="error" showIcon style={{ marginBottom: 14 }} message={t('auth.rutWarn', 'Enable only behind a reverse proxy that strips this header from client requests, and only when server.trusted_proxies is configured.')} description={<span className="hlk-mono" style={{ fontSize: 12 }}>trusted_proxies: {Array.isArray(trusted) && trusted.length ? trusted.join(', ') : t('common.none', 'None')}</span>} />
            <div style={{ marginBottom: 14 }}><Switch checked={draft.rut.enabled} disabled={!Array.isArray(trusted) || !trusted.length} onChange={(v) => setR({ enabled: v })} /> <span style={{ marginLeft: 8 }}>{draft.rut.enabled ? t('common.enabled', 'enabled') : t('common.off', 'off')}</span></div>
            <div style={grid2}>
              <F label={t('auth.header', 'Header')}><Input className="hlk-mono" value={draft.rut.header} onChange={(e) => setR({ header: e.target.value })} placeholder="X-Forwarded-User" /></F>
              <F label={t('auth.autoCreate', 'Auto-create users')}><Checkbox checked={draft.rut.autoCreate} onChange={(e) => setR({ autoCreate: e.target.checked })} /></F>
              <F label={t('auth.defaultRoles', 'Default roles')}><Select mode="multiple" style={{ width: '100%' }} value={draft.rut.defaultRoles ?? []} onChange={(v) => setR({ defaultRoles: v })} options={roleList.map((r) => ({ value: r.id }))} /></F>
            </div>
          </Card>
          <div id="auth-end" style={{ height: 1 }} />
        </div>
      </div>
    </>
  )
}
