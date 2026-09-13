import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { App, Button, Dropdown, Input, Select, Switch, Table } from 'antd'
import { MoreOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, put } from '@/api/client'
import type { CleanupPolicy, Repository, RoutingRule } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { FormatIcon } from '@/components/FormatIcon'
import { TypeTag } from '@/components/TypeTag'
import { Copyable } from '@/components/Copyable'
import { ConfirmDelete, EmptyState, PageHeader, useErrorText } from '@/components/Common'
import { fmtBytes, Num } from '@/components/Format'
import { formatInfo } from '@/theme/tokens'
import { invalidate } from './repoApi'
import { repoBase } from '@/components/UsageSnippets'

export default function Repositories() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [q, setQ] = useState('')
  const [fmt, setFmt] = useState<string | undefined>()
  const [type, setType] = useState<string | undefined>()
  const [toDelete, setToDelete] = useState<Repository | null>(null)
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const rules = useQuery({ queryKey: ['routing-rules'], queryFn: () => get<RoutingRule[]>('routing-rules') })
  const policies = useQuery({ queryKey: ['cleanup-policies'], queryFn: () => get<CleanupPolicy[]>('cleanup-policies') })
  const canWrite = can('app:repositories', 'write')
  const canDelete = can('app:repositories', 'delete')
  const list = useMemo(() => (repos.data ?? []).filter((r) => (!q || r.name.toLowerCase().includes(q.toLowerCase())) && (!fmt || r.format === fmt) && (!type || r.type === type)), [repos.data, q, fmt, type])
  const formats = useMemo(() => Array.from(new Set((repos.data ?? []).map((r) => r.format))).sort(), [repos.data])
  const toggleOnline = useMutation({
    mutationFn: ({ name, online }: { name: string; online: boolean }) => put(`repositories/${name}`, { online }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['repositories'] }),
    onError: (e) => message.error(errText(e)),
  })
  const remove = useMutation({
    mutationFn: (name: string) => del(`repositories/${name}`),
    onSuccess: (_, name) => { message.success(t('repos.deleted', 'Repository {{name}} deleted', { name })); setToDelete(null); qc.invalidateQueries({ queryKey: ['repositories'] }) },
    onError: (e) => message.error(errText(e)),
  })
  const groupsUsing = (name: string) => (repos.data ?? []).filter((r) => r.type === 'group' && r.attributes.group?.members?.includes(name)).map((r) => r.name)

  return (
    <>
      <PageHeader
        title={t('nav.repositories', 'Repositories')} count={repos.data?.length}
        extra={
          <>
            <Input size="small" prefix={<SearchOutlined />} placeholder={t('common.search', 'Search')} value={q} onChange={(e) => setQ(e.target.value)} allowClear style={{ width: 240 }} />
            <Select size="small" allowClear placeholder={t('common.format', 'Format')} value={fmt} onChange={setFmt} style={{ width: 150 }} options={formats.map((f) => ({ value: f, label: formatInfo(f).label }))} />
            <Select size="small" allowClear placeholder={t('common.type', 'Type')} value={type} onChange={setType} style={{ width: 120 }} options={['hosted', 'proxy', 'group'].map((x) => ({ value: x, label: t(`type.${x}`, x) }))} />
            {canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/admin/repositories/new')}>{t('repos.create', 'Create Repository')}</Button>}
          </>
        }
      />
      <div className="hlk-card" style={{ padding: 0 }}>
        <Table<Repository>
          rowKey="name" loading={repos.isLoading} dataSource={list} className="hlk-table" scroll={{ x: 1160 }} size="middle" pagination={{ pageSize: 20, showSizeChanger: false, showTotal: (total, r) => `${r[0]}–${r[1]} / ${total}` }}
          locale={{ emptyText: <EmptyState title={t('repos.empty', 'No repositories yet')} hint={t('repos.emptyHint', 'Create a proxy for Maven Central or npm to get started.')} action={canWrite && <Button type="primary" onClick={() => navigate('/admin/repositories/new')}>{t('repos.create', 'Create Repository')}</Button>} /> }}
          columns={[
            { title: t('common.name', 'Name'), dataIndex: 'name', sorter: (a, b) => a.name.localeCompare(b.name), render: (x: string) => <Link to={`/admin/repositories/${x}`} className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500 }}>{x}</Link> },
            { title: t('common.format', 'Format'), dataIndex: 'format', width: 130, sorter: (a, b) => a.format.localeCompare(b.format), render: (f: string) => <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><FormatIcon format={f} size={18} />{formatInfo(f).label}</span> },
            { title: t('common.type', 'Type'), dataIndex: 'type', width: 110, render: (ty: Repository['type'], r) => <span style={{ display: 'inline-flex', gap: 4 }}><TypeTag type={ty} />{r.attributes.proxy?.blocked && <TypeTag type="blocked" />}</span> },
            { title: t('repos.online', 'Online'), dataIndex: 'online', width: 80, render: (on: boolean, r) => <Switch size="small" checked={on} disabled={!canWrite} onChange={(v) => toggleOnline.mutate({ name: r.name, online: v })} /> },
            { title: t('dashboard.packages', 'Packages'), width: 100, align: 'right', sorter: (a, b) => (a.stats?.packages ?? 0) - (b.stats?.packages ?? 0), render: (_: unknown, r) => <Num value={r.stats?.packages} /> },
            { title: t('asset.size', 'Size'), width: 100, align: 'right', sorter: (a, b) => (a.stats?.size ?? 0) - (b.stats?.size ?? 0), render: (_: unknown, r) => <span className="hlk-num">{fmtBytes(r.stats?.size)}</span> },
            { title: 'URL', render: (_: unknown, r) => <Copyable text={repoBase(r)} style={{ fontSize: 11.5, maxWidth: 360 }} /> },
            { title: t('nav.routing', 'Routing'), width: 120, render: (_: unknown, r) => <span style={{ fontSize: 12 }}>{r.routingRuleId ? rules.data?.find((x) => x.id === r.routingRuleId)?.name ?? '…' : '—'}</span> },
            { title: t('nav.cleanup', 'Cleanup'), width: 90, align: 'center', render: (_: unknown, r) => { const n = (policies.data ?? []).filter((p) => p.repositories?.includes(r.name)).length; return <span style={{ fontSize: 12 }}>{n || '—'}</span> } },
            {
              title: '', width: 44, render: (_: unknown, r) => (
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: [
                      { key: 'edit', label: t('common.edit', 'Edit'), onClick: () => navigate(`/admin/repositories/${r.name}`) },
                      { key: 'browse', label: t('repos.browseContent', 'Browse content'), onClick: () => navigate(`/browse/${r.name}`) },
                      { key: 'usage', label: t('repos.usage', 'Usage'), onClick: () => navigate(`/admin/repositories/${r.name}/usage`) },
                      ...(r.type !== 'hosted' && canWrite ? [{ key: 'inv', label: <span>{t('repos.invalidate', 'Invalidate cache')} <span style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{r.type}</span></span>, onClick: async () => { try { await invalidate(r.name); message.success(t('repos.invalidated', 'Cache invalidated')) } catch (e) { message.error(errText(e)) } } }] : []),
                      ...(canDelete ? [{ type: 'divider' as const }, { key: 'del', danger: true, label: t('common.delete', 'Delete…'), onClick: () => setToDelete(r) }] : []),
                    ],
                  }}
                >
                  <Button type="text" size="small" icon={<MoreOutlined />} />
                </Dropdown>
              ),
            },
          ]}
        />
      </div>
      <ConfirmDelete
        open={!!toDelete} name={toDelete?.name ?? ''} loading={remove.isPending}
        title={<span>{t('repos.deleteTitle', 'Delete repository')} <span className="hlk-mono">{toDelete?.name}</span>?</span>}
        description={
          toDelete && (
            <ul style={{ paddingLeft: 18, margin: 0 }}>
              <li>{t('repos.deleteSize', '{{n}} packages, {{size}} of content. Blobs are reclaimed by the next blob-gc run.', { n: toDelete.stats?.packages ?? 0, size: fmtBytes(toDelete.stats?.size) })}</li>
              {groupsUsing(toDelete.name).length > 0 && <li>{t('repos.deleteGroups', 'Removed from groups: {{list}}', { list: groupsUsing(toDelete.name).join(', ') })}</li>}
              <li>{t('repos.deleteCi', 'Clients using this URL will get 404.')}</li>
            </ul>
          )
        }
        confirmLabel={t('repos.deleteConfirm', 'Delete repository')} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.name) }}
      />
    </>
  )
}
