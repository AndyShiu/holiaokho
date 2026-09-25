import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Button, Input, Select, Table } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get } from '@/api/client'
import type { Repository, SearchHit } from '@/api/types'
import { FormatIcon } from '@/components/FormatIcon'
import { EmptyState, PageHeader } from '@/components/Common'
import { RelTime } from '@/components/Format'
import { AssetDrawer, PackageDrawer } from '@/components/Drawers'
import { formatInfo } from '@/theme/tokens'

export default function Search() {
  const { t } = useTranslation()
  const [sp, setSp] = useSearchParams()
  const q = sp.get('q') ?? ''
  const format = sp.get('format') ?? ''
  const repository = sp.get('repository') ?? ''
  const namespace = sp.get('namespace') ?? ''
  const version = sp.get('version') ?? ''
  const [input, setInput] = useState(q)
  const [page, setPage] = useState(0)
  const [pkg, setPkg] = useState<string | null>(sp.get('package'))
  const [asset, setAsset] = useState<string | null>(null)
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const active = !!(q || format || repository || namespace || version)
  const results = useQuery({ queryKey: ['search', q, format, repository, namespace, version, page], queryFn: () => get<SearchHit[]>('search', { q, format, repository, namespace, version, limit: 50, offset: page * 50 }), enabled: active })
  useEffect(() => setInput(q), [q])
  useEffect(() => {
    const id = setTimeout(() => { if (input !== q) update({ q: input }) }, 400)
    return () => clearTimeout(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input])
  const update = (patch: Record<string, string>) => {
    const next = new URLSearchParams(sp)
    Object.entries(patch).forEach(([k, v]) => (v ? next.set(k, v) : next.delete(k)))
    next.delete('package')
    setSp(next, { replace: true })
    setPage(0)
  }
  const repoOptions = useMemo(() => (repos.data ?? []).filter((r) => !format || r.format === format).map((r) => ({ value: r.name, label: r.name })), [repos.data, format])
  const formats = useMemo(() => Array.from(new Set((repos.data ?? []).map((r) => r.format))).sort(), [repos.data])
  const repoOf = (name: string) => repos.data?.find((r) => r.name === name)

  return (
    <>
      <PageHeader title={t('nav.search', 'Search')} />
      <div className="hlk-card" style={{ marginBottom: 16 }}>
        <Input size="large" autoFocus prefix={<SearchOutlined />} placeholder={t('search.placeholder', 'Search packages… e.g. gson, @babel/core, library/alpine')} value={input} onChange={(e) => setInput(e.target.value)} onPressEnter={() => update({ q: input })} allowClear />
        <div style={{ display: 'flex', gap: 8, marginTop: 12, flexWrap: 'wrap' }}>
          <Select allowClear placeholder={t('common.format', 'Format')} value={format || undefined} onChange={(v) => update({ format: v ?? '', repository: '' })} style={{ width: 180 }} options={formats.map((f) => ({ value: f, label: <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><FormatIcon format={f} size={14} />{formatInfo(f).label}</span> }))} />
          <Select allowClear showSearch placeholder={t('common.repository', 'Repository')} value={repository || undefined} onChange={(v) => update({ repository: v ?? '' })} style={{ width: 220 }} options={repoOptions} />
          <Input placeholder={t('package.namespace', 'Namespace')} value={namespace} onChange={(e) => update({ namespace: e.target.value })} style={{ width: 180 }} allowClear />
          <Input placeholder={t('package.version', 'Version')} value={version} onChange={(e) => update({ version: e.target.value })} style={{ width: 140 }} allowClear />
        </div>
      </div>
      {!active ? (
        <div className="hlk-card">
          <EmptyState title={t('search.emptyTitle', 'Search across all repositories')} hint={<span>{t('search.emptyHint', 'Try')} <code>gson</code>, <code>@babel/core</code>, <code>library/alpine</code></span>} />
        </div>
      ) : (
        <div className="hlk-card" style={{ padding: 0 }}>
          <Table<SearchHit>
            rowKey="id" loading={results.isLoading} dataSource={results.data ?? []} className="hlk-table hlk-clickable" scroll={{ x: 1000 }} size="middle"
            onRow={(r) => ({ onClick: () => setPkg(r.id) })}
            locale={{ emptyText: <EmptyState title={t('search.noResults', 'No packages match')} /> }}
            pagination={{ current: page + 1, pageSize: 50, total: (results.data?.length ?? 0) < 50 ? page * 50 + (results.data?.length ?? 0) : (page + 2) * 50, onChange: (p) => setPage(p - 1), showSizeChanger: false }}
            columns={[
              { title: '', width: 40, render: (_: unknown, r) => <FormatIcon format={r.format} size={18} /> },
              { title: t('common.repository', 'Repository'), dataIndex: 'repository', render: (x: string) => <Link to={`/browse/${x}`} onClick={(e) => e.stopPropagation()} className="hlk-mono" style={{ fontSize: 12 }}>{x}</Link> },
              { title: t('package.namespace', 'Namespace'), dataIndex: 'namespace', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x || '—'}</span> },
              { title: t('common.name', 'Name'), dataIndex: 'name', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12.5, fontWeight: 500 }}>{x}</span> },
              { title: t('package.version', 'Version'), dataIndex: 'version', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x}</span> },
              { title: t('asset.lastDownloaded', 'Last downloaded'), dataIndex: 'lastDownloadedAt', width: 140, render: (x: string) => <RelTime value={x} empty={t('common.never', 'never')} /> },
              { title: t('common.created', 'Created'), dataIndex: 'createdAt', width: 130, render: (x: string) => <RelTime value={x} /> },
            ]}
          />
        </div>
      )}
      <PackageDrawer packageId={pkg} onClose={() => { setPkg(null); if (sp.get('package')) update({}) }} onAsset={(id) => setAsset(id)} />
      <AssetDrawer assetId={asset} repo={undefined} onClose={() => setAsset(null)} />
      {false && <Button />}
    </>
  )
}
