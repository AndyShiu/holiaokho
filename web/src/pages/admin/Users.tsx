import { useMemo, useState } from 'react'
import { Alert, App, Button, Drawer, Form, Input, Modal, Select, Switch, Table, Tag } from 'antd'
import { PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError, del, get, post, put } from '@/api/client'
import type { Role, Token, User } from '@/api/types'
import { useAuth } from '@/auth/AuthContext'
import { ConfirmDelete, PageHeader, useErrorText } from '@/components/Common'
import { PasswordRules, passwordOk } from '@/components/PasswordRules'
import { RelTime } from '@/components/Format'
import { TokenList } from '@/pages/Tokens'

const sourceColor: Record<string, string> = { local: 'default', ldap: 'blue', oidc: 'purple', rut: 'gold' }

export default function Users() {
  const { t } = useTranslation()
  const { can, session, methods } = useAuth()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const errText = useErrorText()
  const [q, setQ] = useState('')
  const [source, setSource] = useState<string | undefined>()
  const [drawer, setDrawer] = useState<'new' | User | null>(null)
  const [pwFor, setPwFor] = useState<User | null>(null)
  const [tokensFor, setTokensFor] = useState<User | null>(null)
  const [toDelete, setToDelete] = useState<User | null>(null)
  const [pw, setPw] = useState('')
  const [failed, setFailed] = useState<string | undefined>()
  const [form] = Form.useForm()
  const users = useQuery({ queryKey: ['users'], queryFn: () => get<User[]>('users') })
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => get<Role[]>('roles') })
  const canWrite = can('app:users', 'write')
  const list = useMemo(() => (users.data ?? []).filter((u) => u.username !== 'anonymous' && (!q || u.username.includes(q) || u.displayName?.toLowerCase().includes(q.toLowerCase()) || u.email?.includes(q)) && (!source || u.source === source)), [users.data, q, source])
  const username = Form.useWatch('username', form)
  const editing = drawer && drawer !== 'new' ? drawer : null

  const save = useMutation({
    mutationFn: (v: any) => (drawer === 'new' ? post('users', { ...v, password: pw }) : put(`users/${editing!.username}`, v)),
    onSuccess: () => { message.success(t('common.saved', 'Saved')); setDrawer(null); setPw(''); qc.invalidateQueries({ queryKey: ['users'] }) },
    onError: (e) => { if (e instanceof ApiError && e.code.startsWith('password.')) setFailed(e.code) },
  })
  const toggleActive = useMutation({ mutationFn: (u: User) => put(`users/${u.username}`, { active: !u.active }), onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }), onError: (e) => message.error(errText(e)) })
  const setPassword = useMutation({
    mutationFn: () => put(`users/${pwFor!.username}/password`, { password: pw }),
    onSuccess: () => { message.success(t('users.pwReset', 'Password updated')); setPwFor(null); setPw('') },
    onError: (e) => { if (e instanceof ApiError && e.code.startsWith('password.')) setFailed(e.code); else message.error(errText(e)) },
  })
  const remove = useMutation({ mutationFn: (u: User) => del(`users/${u.username}`), onSuccess: () => { message.success(t('common.deleted', 'Deleted')); setToDelete(null); qc.invalidateQueries({ queryKey: ['users'] }) }, onError: (e) => message.error(errText(e)) })

  const openEdit = (u: User) => { form.setFieldsValue({ username: u.username, displayName: u.displayName, email: u.email, roles: u.roles, active: u.active }); setDrawer(u) }
  const selfLosingAdmin = (v: any) => editing?.username === session?.username && editing?.roles.includes('admin') && !v.roles?.includes('admin')

  return (
    <>
      <PageHeader title={t('nav.users', 'Users')} count={list.length} extra={<>
        <Input size="small" prefix={<SearchOutlined />} placeholder={t('common.search', 'Search')} value={q} onChange={(e) => setQ(e.target.value)} allowClear style={{ width: 220 }} />
        <Select size="small" allowClear placeholder={t('users.source', 'Source')} value={source} onChange={setSource} style={{ width: 120 }} options={['local', 'ldap', 'oidc', 'rut'].map((s) => ({ value: s }))} />
        {canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setPw(''); setFailed(undefined); setDrawer('new') }}>{t('users.create', 'Create user')}</Button>}
      </>} />
      <div className="hlk-card" style={{ padding: 0 }}>
        <Table<User>
          rowKey="id" loading={users.isLoading} dataSource={list} className="hlk-table" size="middle" pagination={{ pageSize: 25, showSizeChanger: false }}
          columns={[
            { title: t('login.username', 'Username'), dataIndex: 'username', sorter: (a, b) => a.username.localeCompare(b.username), render: (x: string) => <span className="hlk-mono" style={{ fontWeight: 500 }}>{x}</span> },
            { title: t('users.displayName', 'Display name'), dataIndex: 'displayName' },
            { title: 'Email', dataIndex: 'email', render: (x: string) => <span className="hlk-mono" style={{ fontSize: 12 }}>{x || '—'}</span> },
            { title: t('users.source', 'Source'), dataIndex: 'source', width: 90, render: (x: string) => <Tag color={sourceColor[x]}>{x}</Tag> },
            { title: t('nav.roles', 'Roles'), dataIndex: 'roles', render: (rs: string[]) => rs.map((r) => <Tag key={r}>{r}</Tag>) },
            { title: t('users.active', 'Active'), dataIndex: 'active', width: 80, render: (a: boolean, u) => <Switch size="small" checked={a} disabled={!canWrite || u.username === 'admin'} onChange={() => toggleActive.mutate(u)} /> },
            { title: t('common.created', 'Created'), dataIndex: 'createdAt', width: 120, render: (x: string) => <RelTime value={x} /> },
            { title: '', width: 260, render: (_: unknown, u) => canWrite && <span style={{ fontSize: 12, display: 'flex', gap: 10 }}>
              <a onClick={() => openEdit(u)}>{t('common.edit', 'Edit')}</a>
              {u.source === 'local' && <a onClick={() => { setPw(''); setFailed(undefined); setPwFor(u) }}>{t('users.resetPw', 'Reset password')}</a>}
              <a onClick={() => setTokensFor(u)}>{t('nav.tokens', 'Tokens')}</a>
              {u.username !== 'admin' && u.username !== session?.username && can('app:users', 'delete') && <a style={{ color: 'var(--hlk-error)' }} onClick={() => setToDelete(u)}>{t('common.delete', 'Delete')}</a>}
            </span> },
          ]}
        />
      </div>

      <Drawer open={!!drawer} onClose={() => setDrawer(null)} width={560} title={drawer === 'new' ? t('users.create', 'Create user') : t('users.edit', 'Edit {{name}}', { name: editing?.username })} destroyOnClose>
        {save.error && !(save.error instanceof ApiError && save.error.code.startsWith('password.')) ? <Alert type="error" showIcon message={save.error instanceof ApiError && save.error.status === 409 ? t('users.exists', 'That username already exists') : errText(save.error)} style={{ marginBottom: 16 }} /> : null}
        {editing && editing.source !== 'local' && <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('users.externalHint', 'Synced from {{source}}. Roles from the mapping are applied at login; roles set here are added on top.', { source: editing.source })} />}
        <Form form={form} layout="vertical" initialValues={{ active: true, roles: [] }} onFinish={(v) => {
          if (selfLosingAdmin(v)) { Modal.confirm({ title: t('users.selfAdminTitle', 'Remove your own admin role?'), content: t('users.selfAdminHint', 'You will lose access to this page immediately.'), okButtonProps: { danger: true }, onOk: () => save.mutate(v) }); return }
          save.mutate(v)
        }}>
          <Form.Item name="username" label={t('login.username', 'Username')} rules={[{ required: true }, { pattern: /^[A-Za-z0-9._@-]+$/, message: t('repos.nameInvalid', 'Only letters, digits, . _ -') }]}><Input className="hlk-mono" disabled={!!editing} autoComplete="off" /></Form.Item>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Form.Item name="displayName" label={t('users.displayName', 'Display name')}><Input /></Form.Item>
            <Form.Item name="email" label="Email" rules={[{ type: 'email' }]}><Input /></Form.Item>
          </div>
          {drawer === 'new' && (
            <Form.Item label={t('login.password', 'Password')} required>
              <Input.Password value={pw} onChange={(e) => { setPw(e.target.value); setFailed(undefined) }} autoComplete="new-password" />
              <PasswordRules policy={methods?.passwordPolicy} value={pw} username={username} failedCode={failed} />
            </Form.Item>
          )}
          <Form.Item name="roles" label={t('nav.roles', 'Roles')}><Select mode="multiple" options={(roles.data ?? []).map((r) => ({ value: r.id, label: `${r.id} — ${r.name}` }))} /></Form.Item>
          <Form.Item name="active" label={t('users.active', 'Active')} valuePropName="checked"><Switch /></Form.Item>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button type="primary" htmlType="submit" loading={save.isPending} disabled={drawer === 'new' && !passwordOk(methods?.passwordPolicy, pw, username)}>{drawer === 'new' ? t('common.create', 'Create') : t('common.save', 'Save')}</Button>
            <Button onClick={() => setDrawer(null)}>{t('common.cancel', 'Cancel')}</Button>
          </div>
        </Form>
      </Drawer>

      <Modal open={!!pwFor} onCancel={() => setPwFor(null)} title={t('users.resetPwTitle', 'Reset password for {{name}}', { name: pwFor?.username })} okText={t('users.resetPw', 'Reset password')} onOk={() => setPassword.mutate()} okButtonProps={{ disabled: !passwordOk(methods?.passwordPolicy, pw, pwFor?.username) || !pw, loading: setPassword.isPending }} destroyOnClose>
        <Input.Password value={pw} onChange={(e) => { setPw(e.target.value); setFailed(undefined) }} autoComplete="new-password" autoFocus style={{ marginTop: 8 }} />
        <PasswordRules policy={methods?.passwordPolicy} value={pw} username={pwFor?.username} failedCode={failed} />
      </Modal>

      <Drawer open={!!tokensFor} onClose={() => setTokensFor(null)} width={720} title={t('users.tokensOf', 'Tokens of {{name}}', { name: tokensFor?.username })} destroyOnClose>
        {tokensFor && <TokenList base={`users/${tokensFor.username}/tokens`} />}
      </Drawer>

      <ConfirmDelete open={!!toDelete} name={toDelete?.username ?? ''} title={<span>{t('users.deleteTitle', 'Delete user')} <span className="hlk-mono">{toDelete?.username}</span>?</span>} description={t('users.deleteHint', 'Their tokens stop working immediately.')} onCancel={() => setToDelete(null)} onConfirm={() => { if (toDelete) remove.mutate(toDelete) }} loading={remove.isPending} />
      {false && <Token />}
    </>
  )
}
function Token() { return null }
