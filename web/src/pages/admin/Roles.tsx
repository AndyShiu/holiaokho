import { useMemo, useState } from 'react'
import { Alert, App, Button, Drawer, Form, Input, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { del, get, post, put } from '@/api/client'
import type { AuthSettings, Privilege, Role, User } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, useErrorText } from '@/components/Common'
import { PrivilegeEditor, summarise } from '@/components/PrivilegeEditor'

const BUILTIN = ['admin', 'anonymous', 'developer']

export default function Roles() {
  const { t } = useTranslation()
  const { can } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [drawer, setDrawer] = useState<'new' | Role | null>(null)
  const [privs, setPrivs] = useState<Privilege[]>([])
  const [toDelete, setToDelete] = useState<Role | null>(null)
  const [form] = Form.useForm()
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => get<Role[]>('roles') })
  const users = useQuery({ queryKey: ['users'], queryFn: () => get<User[]>('users'), enabled: can('app:users', 'read') })
  const auth = useQuery({ queryKey: ['auth-settings'], queryFn: () => get<AuthSettings>('auth/settings'), enabled: can('app:system', 'read') })
  const canWrite = can('app:roles', 'write')
  const editing = drawer && drawer !== 'new' ? drawer : null
  const usage = (id: string) => {
    const u = (users.data ?? []).filter((x) => x.roles.includes(id)).length
    const l = Object.values(auth.data?.ldap?.roleMapping ?? {}).filter((x) => x === id).length + Object.values(auth.data?.oidc?.roleMapping ?? {}).filter((x) => x === id).length
    return { users: u, mappings: l }
  }
  const save = useMutation({
    mutationFn: (v: any) => (drawer === 'new' ? post('roles', { ...v, privileges: privs }) : put(`roles/${editing!.id}`, { ...v, privileges: privs })),
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); qc.invalidateQueries({ queryKey: ['roles'] }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => del(`roles/${id}`), onSuccess: () => { message.success(t('common.deleted', 'Deleted')); setToDelete(null); qc.invalidateQueries({ queryKey: ['roles'] }) }, onError: (e) => message.error(errText(e)) })
  const summary = useMemo(() => summarise(privs, t as any), [privs, t])

  return (
    <>
      <PageHeader title={t('nav.roles', 'Roles')} count={roles.data?.length} extra={canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setPrivs([{ target: 'repo:*', actions: ['read'] }]); setDrawer('new') }}>{t('roles.create', 'Create role')}</Button>} />
      <div className="hlk-card" style={{ padding: 0 }}>
        <Table<Role>
          rowKey="id" loading={roles.isLoading} dataSource={roles.data ?? []} className="hlk-table hlk-clickable" pagination={false} size="middle"
          onRow={(r) => ({ onClick: () => { form.setFieldsValue({ id: r.id, name: r.name, description: r.description }); setPrivs(r.privileges.map((p) => ({ ...p, actions: [...p.actions] }))); setDrawer(r) } })}
          columns={[
            { title: 'ID', dataIndex: 'id', width: 180, render: (x: string) => <span className="hlk-mono" style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('common.name', 'Name'), dataIndex: 'name', width: 200 },
            { title: t('common.description', 'Description'), dataIndex: 'description', render: (x: string) => <span style={{ color: 'var(--hlk-text-secondary)' }}>{x}</span> },
            { title: '', width: 120, render: (_: unknown, r) => <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>{r.privileges.length} privileges</span> },
            { title: '', width: 90, render: (_: unknown, r) => (BUILTIN.includes(r.id) ? <Tag>{t('roles.builtin', 'built-in')}</Tag> : null) },
            { title: '', width: 80, render: (_: unknown, r) => !BUILTIN.includes(r.id) && can('app:roles', 'delete') && <a style={{ color: 'var(--hlk-error)', fontSize: 12 }} onClick={(e) => { e.stopPropagation(); setToDelete(r) }}>{t('common.delete', 'Delete')}</a> },
          ]}
        />
      </div>
      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={760} title={drawer === 'new' ? t('roles.create', 'Create role') : <span>{t('roles.edit', 'Edit role')} <span className="hlk-mono">{editing?.id}</span></span>} destroyOnClose>
        {save.error ? <Alert type="error" showIcon message={errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        {editing?.id === 'admin' && <Alert type="info" showIcon message={t('roles.adminLocked', 'The admin role always has full access; its privileges cannot be changed.')} style={{ marginBottom: 16 }} />}
        <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} disabled={!canWrite || editing?.id === 'admin'}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Form.Item name="id" label="ID" rules={[{ pattern: /^[A-Za-z0-9._-]*$/, message: t('repos.nameInvalid', 'Only letters, digits, . _ -') }]} extra={drawer === 'new' ? t('roles.idHint', 'Leave empty to derive from the name') : undefined}><Input className="hlk-mono" disabled={!!editing} /></Form.Item>
            <Form.Item name="name" label={t('common.name', 'Name')} rules={[{ required: true }]}><Input /></Form.Item>
          </div>
          <Form.Item name="description" label={t('common.description', 'Description')}><Input /></Form.Item>
          <div className="hlk-section-label" style={{ margin: '8px 0 10px' }}>{t('roles.privileges', 'Privileges')}</div>
          <PrivilegeEditor value={privs} onChange={setPrivs} />
          <div className="hlk-summary" style={{ marginTop: 16 }}>
            <div className="hlk-section-label" style={{ marginBottom: 6 }}>{t('roles.summary', 'What this role can do')}</div>
            {summary.length ? summary.map((s, i) => <div key={i}>• {s}</div>) : <span style={{ color: 'var(--hlk-text-tertiary)' }}>{t('roles.summaryEmpty', 'Nothing yet — add at least one privilege.')}</span>}
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 20 }}>
            <Button type="primary" htmlType="submit" loading={save.isPending} disabled={!privs.length || privs.some((p) => !p.actions.length)}>{t('common.save', 'Save')}</Button>
            <Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button>
            <div style={{ flex: 1 }} />
            {editing && <span style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)' }}>{t('roles.usedBy', 'Used by {{u}} users, {{m}} LDAP/OIDC mappings', usage(editing.id))}</span>}
          </div>
        </Form>
      </Drawer>
      <ConfirmDelete open={!!toDelete} name={toDelete?.id ?? ''} title={<span>{t('roles.deleteTitle', 'Delete role')} <span className="hlk-mono">{toDelete?.id}</span>?</span>} description={toDelete && t('roles.deleteHint', 'Used by {{u}} users. They lose these privileges immediately.', { u: usage(toDelete.id).users })} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete.id) }} loading={remove.isPending} />
    </>
  )
}
