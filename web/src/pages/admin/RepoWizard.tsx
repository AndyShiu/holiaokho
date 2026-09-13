import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, App, Button, Input, Steps } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError, get, post } from '@/api/client'
import type { CleanupPolicy, RepoType, Repository, RoutingRule, Status } from '@/api/types'
import { FormatIcon } from '@/components/FormatIcon'
import { TypeTag } from '@/components/TypeTag'
import { PageHeader, useErrorText } from '@/components/Common'
import { RepoForm, initialValues, type RepoFormValues } from '@/components/RepoForm'
import { formatInfo } from '@/theme/tokens'
import { snippetsFor } from '@/components/UsageSnippets'
import { syncCleanup, toPayload } from './repoApi'

export default function RepoWizard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [sp] = useSearchParams()
  const [format, setFormat] = useState<string | null>(sp.get('format'))
  const [type, setType] = useState<RepoType | null>((sp.get('type') as RepoType) || null)
  const [filter, setFilter] = useState('')
  const [values, setValues] = useState<RepoFormValues | null>(format && type ? initialValues(format, type) : null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status') })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const rules = useQuery({ queryKey: ['routing-rules'], queryFn: () => get<RoutingRule[]>('routing-rules') })
  const policies = useQuery({ queryKey: ['cleanup-policies'], queryFn: () => get<CleanupPolicy[]>('cleanup-policies') })
  const formats = useMemo(() => (status.data?.formats ?? Object.keys(formatInfo)).filter((f) => !filter || f.includes(filter.toLowerCase()) || formatInfo(f).label.toLowerCase().includes(filter.toLowerCase())), [status.data, filter])
  const step = !format ? 0 : !type ? 1 : 2
  const hasMembers = format ? (repos.data ?? []).some((r) => r.format === format && r.type !== 'group') : false

  const pickType = (ty: RepoType) => {
    setType(ty)
    setValues(initialValues(format!, ty))
  }
  const preview = useMemo(() => {
    if (!values || !format || !type) return ''
    const fake: Repository = { id: '', name: values.name || `${format}-${type}`, format, type, storageId: '', online: true, attributes: values.attributes, createdAt: '', updatedAt: '' }
    return snippetsFor(fake, t as any)[0]?.code ?? ''
  }, [values, format, type, t])

  const submit = async () => {
    if (!values || !format || !type) return
    setBusy(true)
    setError(null)
    try {
      await post('repositories', toPayload(values, rules.data ?? [], format, type))
      await syncCleanup(values.name, values.cleanupPolicies, policies.data ?? [])
      qc.invalidateQueries({ queryKey: ['repositories'] })
      qc.invalidateQueries({ queryKey: ['cleanup-policies'] })
      message.success(t('repos.created', 'Repository {{name}} created', { name: values.name }))
      navigate(`/admin/repositories/${values.name}/usage`)
    } catch (e) {
      setError(e)
    } finally {
      setBusy(false)
    }
  }
  const nameConflict = error instanceof ApiError && error.status === 409

  const typeCards: { ty: RepoType; title: string; desc: string; when: string }[] = [
    { ty: 'hosted', title: t('type.hosted', 'Hosted'), desc: t('wizard.hostedDesc', 'Your own artifacts, uploaded by developers or CI.'), when: t('wizard.hostedWhen', 'Internal libraries, release builds, images you build.') },
    { ty: 'proxy', title: t('type.proxy', 'Proxy'), desc: t('wizard.proxyDesc', 'Caches a remote registry. Downloaded once, served locally afterwards.'), when: t('wizard.proxyWhen', 'Maven Central, npmjs, Docker Hub, PyPI…') },
    { ty: 'group', title: t('type.group', 'Group'), desc: t('wizard.groupDesc', 'One URL that merges several hosted and proxy repositories.'), when: t('wizard.groupWhen', 'Give clients a single address.') },
  ]

  return (
    <>
      <PageHeader title={<span><span style={{ color: 'var(--hlk-text-tertiary)', fontWeight: 400 }}>{t('nav.repositories', 'Repositories')} / </span>{t('wizard.title', 'Create')}</span>} extra={<Button onClick={() => navigate('/admin/repositories')}>{t('common.cancel', 'Cancel')}</Button>} />
      <div className="hlk-wizard-grid">
        <div>
          <Steps
            current={step} size="small" style={{ marginBottom: 24 }}
            onChange={(s) => { if (s === 0) { setFormat(null); setType(null); setValues(null) } else if (s === 1 && format) { setType(null); setValues(null) } }}
            items={[
              { title: t('wizard.step1', 'Format'), description: format ? <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><FormatIcon format={format} size={12} />{format}</span> : undefined },
              { title: t('wizard.step2', 'Type'), description: type ? <TypeTag type={type} small /> : undefined, disabled: !format },
              { title: t('wizard.step3', 'Settings'), disabled: !type },
            ]}
          />
          {step === 0 && (
            <>
              <Input prefix={<SearchOutlined />} placeholder={t('wizard.filterFormats', 'Filter formats')} value={filter} onChange={(e) => setFilter(e.target.value)} allowClear style={{ marginBottom: 16, width: 320 }} autoFocus />
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', gap: 10 }}>
                {formats.map((f) => (
                  <div key={f} className="hlk-optioncard" onClick={() => setFormat(f)} style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                    <FormatIcon format={f} size={28} />
                    <div><div style={{ fontWeight: 500, fontSize: 13 }}>{formatInfo(f).label}</div><div className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{f}</div></div>
                  </div>
                ))}
              </div>
            </>
          )}
          {step === 1 && (
            <div style={{ display: 'grid', gap: 12 }}>
              {typeCards.map((c) => {
                const disabled = c.ty === 'group' && !hasMembers
                return (
                  <div key={c.ty} className="hlk-optioncard" onClick={() => !disabled && pickType(c.ty)} style={{ opacity: disabled ? 0.5 : 1, cursor: disabled ? 'not-allowed' : 'pointer', padding: '18px 20px' }}>
                    <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 4 }}><TypeTag type={c.ty} /><b style={{ fontSize: 15 }}>{c.title}</b></div>
                    <div style={{ fontSize: 13 }}>{c.desc}</div>
                    <div style={{ fontSize: 12, color: 'var(--hlk-text-secondary)', marginTop: 4 }}>{disabled ? t('wizard.groupNeedsMembers', 'Create a hosted or proxy {{format}} repository first.', { format }) : c.when}</div>
                  </div>
                )
              })}
            </div>
          )}
          {step === 2 && values && format && type && (
            <>
              <h2 style={{ fontSize: 22, fontWeight: 600, margin: '0 0 16px' }}>{t('wizard.configure', 'Configure {{format}} {{type}}', { format: formatInfo(format).label, type })}</h2>
              {error && !nameConflict ? <Alert type="error" showIcon message={errText(error)} style={{ marginBottom: 16 }} /> : null}
              {nameConflict && <Alert type="error" showIcon message={t('repos.nameExists', 'A repository named {{name}} already exists.', { name: values.name })} style={{ marginBottom: 16 }} />}
              <RepoForm format={format} type={type} value={values} onChange={setValues} />
              <div style={{ display: 'flex', gap: 8 }}>
                <Button onClick={() => { setType(null); setValues(null) }}>{t('common.back', 'Back')}</Button>
                <Button type="primary" style={{ flex: 1 }} loading={busy} disabled={!values.name || !/^[A-Za-z0-9._-]+$/.test(values.name) || (type === 'group' && !(values.attributes.group?.members?.length))} onClick={submit}>
                  {t('wizard.create', 'Create {{name}}', { name: values.name || '…' })}
                </Button>
              </div>
            </>
          )}
        </div>
        {step === 2 && (
          <div style={{ position: 'sticky', top: 76 }}>
            <div style={{ background: '#131923', color: '#C9CFD8', borderRadius: 10, padding: 16 }}>
              <div className="hlk-section-label" style={{ color: '#6F7A8A', marginBottom: 8 }}>{t('wizard.preview', 'After creation, clients use it like this')}</div>
              <pre className="hlk-mono" style={{ margin: 0, fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{preview}</pre>
            </div>
          </div>
        )}
      </div>
    </>
  )
}
