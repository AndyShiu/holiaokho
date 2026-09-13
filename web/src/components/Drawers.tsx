import { useState } from 'react'
import { Link } from 'react-router-dom'
import { App, Button, Drawer, Table, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get } from '@/api/client'
import type { Asset, Package, Repository } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, KV, useErrorText } from './Common'
import { Copyable } from './Copyable'
import { fmtBytes, AbsTime, RelTime } from './Format'
import { repoBase } from './UsageSnippets'

// GET /assets/{id} answers with the asset plus its repository and a ready-made
// download URL, not a bare asset.
interface AssetView {
  asset: Asset
  repository: string
  downloadUrl: string
}

export function AssetDrawer({ assetId, repo: repoProp, onClose, onDeleted }: { assetId: string | null; repo?: Repository; onClose: () => void; onDeleted?: () => void }) {
  const { t } = useTranslation()
  const { canRepo } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [confirm, setConfirm] = useState(false)
  const q = useQuery({ queryKey: ['asset', assetId], queryFn: () => get<AssetView>(`assets/${assetId}`), enabled: !!assetId })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories'), enabled: !!assetId && !repoProp })
  const a = q.data?.asset
  const repo = repoProp ?? repos.data?.find((r) => r.name === q.data?.repository)
  const url = q.data?.downloadUrl ?? (repo && a ? `${repoBase(repo)}/${a.path}` : '')
  const canDelete = repo ? canRepo(repo.name, repo.format, 'delete') : false
  const doDelete = useMutation({
    mutationFn: () => del(`assets/${assetId}`),
    onSuccess: () => { message.success(t('common.deleted', 'Deleted')); setConfirm(false); onDeleted?.(); onClose() },
    onError: (e) => message.error(errText(e)),
  })
  return (
    <Drawer open={!!assetId} onClose={onClose} width={600} title={<span className="hlk-mono" style={{ fontSize: 15, wordBreak: 'break-all', whiteSpace: 'normal' }}>{a?.path ?? '…'}</span>} destroyOnClose>
      {a && (
        <>
          <KV items={[
            [t('asset.size', 'Size'), fmtBytes(a.size)],
            [t('asset.contentType', 'Content type'), <span className="hlk-mono">{a.contentType || '—'}</span>],
            [t('asset.sha256', 'sha256'), <Copyable text={a.blobDigest} />],
            [t('asset.package', 'Package'), a.packageId ? <Link to={`/search?package=${a.packageId}`}>{a.packageId}</Link> : '—'],
            [t('common.created', 'Created'), <AbsTime value={a.createdAt} />],
            [t('common.updated', 'Updated'), <AbsTime value={a.updatedAt} />],
            [t('asset.lastDownloaded', 'Last downloaded'), <RelTime value={a.lastDownloadedAt} empty={t('common.never', 'never')} />],
            ...(repo?.type !== 'hosted' ? [[t('asset.cacheExpires', 'Cache expires'), a.cacheExpiresAt ? <RelTime value={a.cacheExpiresAt} /> : t('asset.cacheForever', 'never (cached forever)')] as [React.ReactNode, React.ReactNode]] : []),
            ...(a.negative ? [[t('asset.negative', 'Negative cache'), <Tag color="warning">{t('asset.negativeHint', 'upstream returned 404')}</Tag>] as [React.ReactNode, React.ReactNode]] : []),
          ]} />
          {url && (
            <div style={{ marginTop: 20, background: 'var(--hlk-card-alt)', border: '1px solid var(--hlk-border)', borderRadius: 8, padding: '10px 12px' }}>
              <div className="hlk-section-label" style={{ marginBottom: 6 }}>{t('asset.downloadUrl', 'Download URL')}</div>
              <Copyable text={url} block style={{ fontSize: 12 }} />
            </div>
          )}
          <div style={{ display: 'flex', gap: 8, marginTop: 24, alignItems: 'center' }}>
            {url && <Button type="primary" href={url} target="_blank">{t('common.download', 'Download')}</Button>}
            {a.packageId && <Button onClick={() => { window.location.hash = '' }} href={`/ui/search?package=${a.packageId}`}>{t('asset.viewPackage', 'View package')}</Button>}
            <div style={{ flex: 1 }} />
            {canDelete ? <Button danger onClick={() => setConfirm(true)}>{t('common.delete', 'Delete')}</Button> : <span style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)' }}>{t('asset.deleteNeedsLogin', 'Deleting requires permission')}</span>}
          </div>
          <ConfirmDelete open={confirm} name={a.path?.split('/').pop() || a.path || ''} typeToConfirm={false} title={t('asset.deleteTitle', 'Delete this file?')} description={a.path} onCancel={() => setConfirm(false)} onConfirm={() => doDelete.mutate()} loading={doDelete.isPending} />
        </>
      )}
    </Drawer>
  )
}

interface PackageView { package: Package; assets: Asset[]; repository: string; format: string }

interface Referrer {
  digest: string
  mediaType: string
  artifactType: string
  size: number
  kind: 'signature' | 'sbom' | 'attestation' | 'other'
  annotations?: Record<string, string>
}

// Whether an image carries a signature is the thing a reader wants to spot
// without having to read a media type, so each kind gets a colour of its own.
const referrerColour: Record<string, string> = {
  signature: 'green', sbom: 'blue', attestation: 'purple', other: 'default',
}

export function PackageDrawer({ packageId, onClose, onDeleted, onAsset }: { packageId: string | null; onClose: () => void; onDeleted?: () => void; onAsset?: (id: string) => void }) {
  const { t } = useTranslation()
  const { canRepo } = useAuth()
  const { message } = App.useApp()
  const errText = useErrorText()
  const qc = useQueryClient()
  const [confirm, setConfirm] = useState(false)
  const q = useQuery({ queryKey: ['package', packageId], queryFn: () => get<PackageView>(`packages/${packageId}`), enabled: !!packageId })
  const repos = useQuery({ queryKey: ['repositories'], queryFn: () => get<Repository[]>('repositories') })
  const v = q.data
  // Only Docker images can carry attachments, so do not ask for anything else.
  const refs = useQuery({
    queryKey: ['package-referrers', packageId],
    queryFn: () => get<Referrer[]>(`packages/${packageId}/referrers`),
    enabled: !!packageId && q.data?.format === 'docker',
  })
  const p = v?.package
  const repo = repos.data?.find((r) => r.name === v?.repository)
  const canDelete = v ? canRepo(v.repository, v.format, 'delete') : false
  const doDelete = useMutation({
    mutationFn: () => del(`packages/${packageId}`),
    onSuccess: () => { message.success(t('common.deleted', 'Deleted')); setConfirm(false); qc.invalidateQueries({ queryKey: ['search'] }); onDeleted?.(); onClose() },
    onError: (e) => message.error(errText(e)),
  })
  const title = p ? `${p.namespace ? p.namespace + '/' : ''}${p.name}@${p.version}` : '…'
  const attrs = Object.entries(p?.attrs ?? {}).filter(([, val]) => typeof val !== 'object')
  return (
    <Drawer open={!!packageId} onClose={onClose} width={640} title={<span className="hlk-mono" style={{ fontSize: 15, wordBreak: 'break-all', whiteSpace: 'normal' }}>{title}</span>} destroyOnClose>
      {v && p && (
        <>
          <KV items={[
            [t('nav.repositories', 'Repository'), <Link to={`/browse/${v.repository}`}>{v.repository}</Link>],
            [t('common.format', 'Format'), v.format],
            [t('common.created', 'Created'), <AbsTime value={p.createdAt} />],
            [t('asset.lastDownloaded', 'Last downloaded'), <RelTime value={p.lastDownloadedAt} empty={t('common.never', 'never')} />],
            ...attrs.slice(0, 12).map(([k, val]) => [k, <span className="hlk-mono" style={{ fontSize: 12 }}>{String(val)}</span>] as [React.ReactNode, React.ReactNode]),
          ]} />
          <div className="hlk-section-label" style={{ margin: '20px 0 8px' }}>{t('package.assets', 'Assets')} · {v.assets.length}</div>
          <Table<Asset>
            size="small" rowKey="id" pagination={false} dataSource={v.assets} className="hlk-table hlk-clickable"
            onRow={(a) => ({ onClick: () => onAsset?.(a.id) })}
            columns={[
              { title: t('asset.path', 'Path'), dataIndex: 'path', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12, wordBreak: 'break-all' }}>{x}</span> },
              { title: t('asset.size', 'Size'), dataIndex: 'size', width: 90, render: (x: number) => fmtBytes(x) },
              { title: '', width: 80, render: (_: unknown, a: Asset) => repo ? <a href={`${repoBase(repo)}/${a.path}`} target="_blank" onClick={(e) => e.stopPropagation()}>{t('common.download', 'Download')}</a> : null },
            ]}
          />
          {!!refs.data?.length && (
            <>
              <div className="hlk-section-label" style={{ margin: '20px 0 8px' }}>
                {t('package.attachments', 'Attachments')} · {refs.data.length}
              </div>
              <p style={{ margin: '0 0 10px', fontSize: 12, color: 'var(--hlk-text-secondary)' }}>
                {t('package.attachmentsHint', 'Signatures, SBOMs and attestations that name this image as their subject.')}
              </p>
              <Table<Referrer>
                size="small" rowKey="digest" pagination={false} dataSource={refs.data} className="hlk-table"
                columns={[
                  {
                    title: t('package.attachmentKind', 'Kind'), dataIndex: 'kind', width: 120,
                    render: (k: string) => <Tag color={referrerColour[k] ?? 'default'} style={{ marginInlineEnd: 0 }}>{t(`package.kind.${k}`, k)}</Tag>,
                  },
                  {
                    title: t('package.attachmentType', 'Artifact type'), dataIndex: 'artifactType',
                    render: (x: string, r: Referrer) => <span className="hlk-mono" style={{ fontSize: 11.5, wordBreak: 'break-all' }}>{x || r.mediaType}</span>,
                  },
                  { title: t('asset.size', 'Size'), dataIndex: 'size', width: 90, render: (x: number) => fmtBytes(x) },
                ]}
              />
            </>
          )}
          {canDelete && (
            <div style={{ marginTop: 20, display: 'flex', justifyContent: 'flex-end' }}>
              <Button danger onClick={() => setConfirm(true)}>{t('package.deleteVersion', 'Delete this version')}</Button>
            </div>
          )}
          <ConfirmDelete open={confirm} name={p.version} title={t('package.deleteTitle', 'Delete {{name}}?', { name: title })} description={t('package.deleteHint', 'All {{n}} assets of this version are removed. Blobs are reclaimed by the next blob-gc run.', { n: v.assets.length })} onCancel={() => setConfirm(false)} onConfirm={() => doDelete.mutate()} loading={doDelete.isPending} />
        </>
      )}
    </Drawer>
  )
}
