import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, App, Button, Input, Modal, Popconfirm, Tag, Tooltip } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, post } from '@/api/client'
import type { BlockedDownload } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { EmptyState, useErrorText } from '@/components/Common'
import { FormatIcon } from '@/components/FormatIcon'
import { Num, RelTime } from '@/components/Format'
import { SortableTable } from '@/components/SortableTable'

// BlockedDownloads lists the downloads refused because the package is known
// to be malicious: who asked for what, how often. Someone asking is the news
// — a build depends on it, or a name was mistyped the way its author hoped.
export function BlockedDownloads() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const qc = useQueryClient()
  const admin = can('app:system', 'write')
  const q = useQuery({ queryKey: ['vuln-blocked'], queryFn: () => api<{ blocking: boolean; items: BlockedDownload[] }>('vulnerabilities/blocked') })
  const [allowing, setAllowing] = useState<BlockedDownload | null>(null)
  const [reason, setReason] = useState('')

  const refresh = () => qc.invalidateQueries({ queryKey: ['vuln-blocked'] })
  const allow = async () => {
    if (!allowing) return
    try {
      await post('vulnerabilities/allowed', { purl: allowing.purl, reason: reason.trim() })
      message.success(t('blocked.allowedMsg', '{{name}} {{version}} can be downloaded again', { name: allowing.name, version: allowing.version }))
      setAllowing(null)
      setReason('')
      refresh()
    } catch (e) {
      message.error(errText(e))
    }
  }
  const disallow = async (b: BlockedDownload) => {
    try {
      await api('vulnerabilities/allowed', { method: 'DELETE', query: { purl: b.purl } })
      message.success(t('blocked.disallowedMsg', '{{name}} {{version}} is blocked again', { name: b.name, version: b.version }))
      refresh()
    } catch (e) {
      message.error(errText(e))
    }
  }

  return (
    <>
      {q.data && !q.data.blocking && (
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('blocked.off', 'Blocking malicious packages is turned off in the server configuration (vulnerabilities.block_malicious).')} />
      )}
      <div style={{ fontSize: 12.5, color: 'var(--hlk-text-secondary)', marginBottom: 12, maxWidth: 820 }}>
        {t('blocked.intro', 'Downloads of packages OSV lists as malicious are refused, from the cache and from upstream. Whatever asked for one — a build, a lock file, a developer — should be checked. The first block of each package is sent by email and as a package.blocked webhook.')}
      </div>
      <div className="hlk-card" style={{ padding: 0 }}>
        <SortableTable<BlockedDownload>
          rowKey={(r) => `${r.purl}|${r.repository}`} loading={q.isLoading} dataSource={q.data?.items ?? []} className="hlk-table" scroll={{ x: 1000 }} size="middle"
          locale={{ emptyText: <EmptyState title={t('blocked.none', 'No malicious package has been requested')} /> }}
          pagination={{ pageSize: 50, showSizeChanger: false, hideOnSinglePage: true }}
          columns={[
            {
              title: t('vulns.package', 'Package'), dataIndex: 'name', width: 260, sortValue: (r) => `${r.name}@${r.version}`, render: (_: unknown, r) => (
                <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', whiteSpace: 'nowrap' }}>
                  <FormatIcon format={r.format} size={16} />
                  <span className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500 }}>{r.name}</span>
                  <span className="hlk-mono" style={{ fontSize: 12 }}>{r.version}</span>
                </span>
              ),
            },
            { title: t('common.repository', 'Repository'), dataIndex: 'repository', width: 150, render: (x: string) => <Link to={`/browse/${x}`} className="hlk-mono" style={{ fontSize: 12 }}>{x}</Link> },
            {
              title: t('blocked.record', 'Listed as malicious by'), dataIndex: 'id', width: 230, render: (_: unknown, r) => (
                <span style={{ display: 'inline-flex', flexDirection: 'column' }}>
                  <a className="hlk-mono" style={{ fontSize: 12 }} href={`https://osv.dev/vulnerability/${encodeURIComponent(r.id)}`} target="_blank" rel="noreferrer">{r.id}</a>
                  {r.summary && <span style={{ fontSize: 11.5, color: 'var(--hlk-text-tertiary)' }}>{r.summary}</span>}
                </span>
              ),
            },
            { title: t('blocked.attempts', 'Attempts'), dataIndex: 'attempts', width: 100, align: 'right', render: (x: number) => <Num value={x} /> },
            { title: t('blocked.lastUser', 'Last requested by'), dataIndex: 'lastUser', width: 150, render: (x: string) => x ? <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> : <span style={{ color: 'var(--hlk-text-tertiary)', fontSize: 12 }}>{t('blocked.anonymous', 'anonymous')}</span> },
            { title: t('blocked.lastAt', 'Last blocked'), dataIndex: 'lastAt', width: 120, render: (x: string) => <RelTime value={x} /> },
            { title: t('blocked.firstAt', 'First blocked'), dataIndex: 'firstAt', width: 120, render: (x: string) => <RelTime value={x} /> },
            {
              title: t('blocked.status', 'Status'), key: 'status', width: 110, sortValue: (r) => (r.allowed ? 1 : 0), render: (_: unknown, r) => r.allowed
                ? <Tooltip title={t('blocked.allowedBy', 'Allowed by {{by}}: {{reason}}', { by: r.allowed.by, reason: r.allowed.reason })}><Tag color="green">{t('blocked.allowed', 'Allowed')}</Tag></Tooltip>
                : <Tag color="red">{t('blocked.blocked', 'Blocked')}</Tag>,
            },
            ...(admin ? [{
              title: '', key: 'action', width: 120, sortable: false as const, render: (_: unknown, r: BlockedDownload) => r.allowed
                ? <Popconfirm title={t('blocked.disallowConfirm', 'Block {{name}} {{version}} again?', { name: r.name, version: r.version })} onConfirm={() => disallow(r)}><Button size="small">{t('blocked.disallow', 'Block again')}</Button></Popconfirm>
                : <Button size="small" danger onClick={() => { setAllowing(r); setReason('') }}>{t('blocked.allow', 'Allow…')}</Button>,
            }] : []),
          ]}
        />
      </div>
      <Modal
        open={!!allowing} onCancel={() => setAllowing(null)} onOk={allow} okButtonProps={{ danger: true, disabled: !reason.trim() }}
        title={t('blocked.allowTitle', 'Allow {{name}} {{version}}?', { name: allowing?.name, version: allowing?.version })} okText={t('blocked.allowOk', 'Allow downloads')}
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={t('blocked.allowWarn', 'OSV lists this version as malicious ({{id}}). Allow it only if you have checked that the listing is wrong. It is allowed in every repository, and the decision is recorded in the audit log.', { id: allowing?.id })} />
        <div className="hlk-section-label" style={{ marginBottom: 6 }}>{t('blocked.reason', 'Reason')}</div>
        <Input.TextArea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} placeholder={t('blocked.reasonPlaceholder', 'Why this listing is wrong, and who checked')} />
      </Modal>
    </>
  )
}
