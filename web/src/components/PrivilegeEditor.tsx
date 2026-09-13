import { Button, Input, Select } from 'antd'
import { DeleteOutlined, CopyOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get } from '@/api/client'
import type { ContentSelector, Privilege, Repository, Status } from '@/api/types'
import { formatInfo } from '@/theme/tokens'

const APP_AREAS = ['repositories', 'storages', 'users', 'roles', 'tasks', 'system', 'search', 'status', '*']
const ACTIONS = ['read', 'write', 'delete', 'admin']

function split(target: string): { kind: string; value: string } {
  if (target === '*') return { kind: '*', value: '' }
  const i = target.indexOf(':')
  if (i < 0) return { kind: 'repo', value: target }
  return { kind: target.slice(0, i), value: target.slice(i + 1) }
}

export function summarise(privs: Privilege[], t: (k: string, d: string, o?: any) => string): string[] {
  const out: string[] = []
  for (const p of privs) {
    const { kind, value } = split(p.target)
    const acts = p.actions.includes('*') ? t('priv.allActions', 'full control') : p.actions.join(' / ')
    if (kind === '*') out.push(t('priv.sumAll', '{{acts}} over the entire system', { acts }))
    // Wildcards read badly when substituted into the named phrasing, so they
    // get their own sentence.
    else if (kind === 'app') out.push(value === '*' ? t('priv.sumAppAll', '{{acts}} over every application area', { acts }) : t('priv.sumApp', '{{acts}}: {{area}} administration', { acts, area: value }))
    else if (kind === 'repo') out.push(value === '*' ? t('priv.sumRepoAll', '{{acts}} on every repository', { acts }) : t('priv.sumRepo', '{{acts}} on repository {{name}}', { acts, name: value }))
    else if (kind === 'format') out.push(value === '*' ? t('priv.sumRepoAll', '{{acts}} on every repository', { acts }) : t('priv.sumFormat', '{{acts}} on all {{format}} repositories', { acts, format: value }))
    else if (kind === 'selector') out.push(t('priv.sumSelector', '{{acts}} on content matching selector {{sel}}', { acts, sel: value }))
  }
  return out
}

export function PrivilegeEditor({ value, onChange }: { value: Privilege[]; onChange: (v: Privilege[]) => void }) {
  const { t } = useTranslation()
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const selectors = useQuery({ queryKey: ['content-selectors'], queryFn: () => get<ContentSelector[]>('content-selectors') })
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status') })
  const set = (i: number, p: Privilege) => onChange(value.map((x, j) => (j === i ? p : x)))
  const kinds = [
    { value: '*', label: t('priv.kind.all', 'Entire system') },
    { value: 'app', label: t('priv.kind.app', 'Application area') },
    { value: 'repo', label: t('priv.kind.repo', 'Repository') },
    { value: 'format', label: t('priv.kind.format', 'All repos of a format') },
    { value: 'selector', label: t('priv.kind.selector', 'Content selector') },
  ]
  return (
    <div>
      <div style={{ display: 'grid', gridTemplateColumns: '150px 1fr 230px 60px', gap: 8, padding: '0 0 6px', fontSize: 10, letterSpacing: '.06em', color: 'var(--hlk-text-tertiary)' }} className="hlk-mono">
        <span>{t('priv.target', 'TARGET')}</span><span>{t('priv.value', 'VALUE')}</span><span>{t('priv.actions', 'ACTIONS')}</span><span />
      </div>
      {value.map((p, i) => {
        const { kind, value: v } = split(p.target)
        const star = p.actions.includes('*')
        const valueInput = (() => {
          if (kind === '*') return <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12, lineHeight: '32px' }}>—</span>
          if (kind === 'app') return <Select size="small" value={v} onChange={(x) => set(i, { ...p, target: `app:${x}` })} options={APP_AREAS.map((a) => ({ value: a }))} className="hlk-mono" />
          if (kind === 'repo') return <Select size="small" showSearch value={v} onChange={(x) => set(i, { ...p, target: `repo:${x}` })} options={[{ value: '*', label: `* (${t('priv.allRepos', 'every repository')})` }, ...(repos.data ?? []).map((r) => ({ value: r.name }))]} className="hlk-mono" />
          if (kind === 'format') return <Select size="small" value={v} onChange={(x) => set(i, { ...p, target: `format:${x}` })} options={[{ value: '*' }, ...(status.data?.formats ?? []).map((f) => ({ value: f, label: formatInfo(f).label }))]} />
          if (kind === 'selector') {
            const [sel, repo] = v.split('@')
            return (
              <span style={{ display: 'grid', gridTemplateColumns: '1fr 20px 1fr', gap: 4, alignItems: 'center' }}>
                <Select size="small" value={sel || undefined} placeholder={t('nav.selectors', 'Selector')} onChange={(x) => set(i, { ...p, target: `selector:${x}@${repo || '*'}` })} options={(selectors.data ?? []).map((s) => ({ value: s.name }))} className="hlk-mono" notFoundContent={<span style={{ fontSize: 12 }}>{t('priv.noSelectors', 'Create a content selector first')}</span>} />
                <span style={{ textAlign: 'center', color: 'var(--hlk-text-tertiary)' }}>@</span>
                <Select size="small" showSearch value={repo || '*'} onChange={(x) => set(i, { ...p, target: `selector:${sel}@${x}` })} options={[{ value: '*' }, ...(repos.data ?? []).map((r) => ({ value: r.name }))]} className="hlk-mono" />
              </span>
            )
          }
          return <Input size="small" value={v} />
        })()
        return (
          <div key={i} style={{ display: 'grid', gridTemplateColumns: '150px 1fr 230px 60px', gap: 8, alignItems: 'center', padding: '6px 0', borderTop: '1px solid var(--hlk-row)' }}>
            <Select size="small" value={kind} onChange={(k) => set(i, { ...p, target: k === '*' ? '*' : `${k}:${k === 'app' ? 'repositories' : '*'}` })} options={kinds} />
            {valueInput}
            <span style={{ display: 'flex', gap: 4 }}>
              {[...ACTIONS, '*'].map((a) => (
                <button key={a} type="button" className={`hlk-privchip${p.actions.includes(a) ? ' on' : ''}`} disabled={star && a !== '*'} onClick={() => set(i, { ...p, actions: a === '*' ? (star ? [] : ['*']) : p.actions.includes(a) ? p.actions.filter((x) => x !== a) : [...p.actions.filter((x) => x !== '*'), a] })}>
                  {a}
                </button>
              ))}
            </span>
            <span style={{ display: 'flex', gap: 2 }}>
              <Button type="text" size="small" icon={<CopyOutlined />} onClick={() => onChange([...value.slice(0, i + 1), { ...p, actions: [...p.actions] }, ...value.slice(i + 1)])} />
              <Button type="text" size="small" icon={<DeleteOutlined />} onClick={() => onChange(value.filter((_, j) => j !== i))} />
            </span>
          </div>
        )
      })}
      <Button type="link" size="small" icon={<PlusOutlined />} onClick={() => onChange([...value, { target: 'repo:*', actions: ['read'] }])} style={{ paddingLeft: 0, marginTop: 6 }}>{t('priv.add', 'Add a row')}</Button>
    </div>
  )
}
