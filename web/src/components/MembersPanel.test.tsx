import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MembersPanel } from './MembersPanel'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api }))

const auth = vi.hoisted(() => ({ role: 'admin' as string | undefined, id: 'me' }))
vi.mock('../api/authEvents', () => ({
  AUTH_SESSION_CHANGED_EVENT: 'anubis:auth-session-changed',
  getLastKnownUser: () => (auth.role ? { id: auth.id, role: auth.role } : null),
}))

const owner = {
  id: 'owner-1',
  email: 'admin@example.com',
  name: 'Administrator',
  role: 'owner',
  workspace: 'default',
  source: 'local',
  disabled: false,
  configured_admin: true,
  created_at: '2026-10-01T00:00:00Z',
}
const meMember = { ...owner, id: 'me', email: 'me@example.com', name: 'Me', role: 'admin', configured_admin: false }
const editor = { ...owner, id: 'ed-1', email: 'ed@example.com', name: 'Ed', role: 'editor', configured_admin: false }
const sso = { ...editor, id: 'sso-1', email: 'sso@example.com', name: 'SSO', role: 'viewer', source: 'oidc', disabled: true }
const invitation = {
  id: 'inv-1',
  email: 'new@example.com',
  role: 'viewer',
  workspace: 'default',
  invited_by: 'admin@example.com',
  created_at: '2026-10-20T00:00:00Z',
  expires_at: '2026-10-27T00:00:00Z',
}

function mockLists(members: unknown[], invitations: unknown[] = []) {
  api.get.mockImplementation((path: string) => Promise.resolve(path === '/members' ? members : invitations))
}

describe('MembersPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.role = 'admin'
    auth.id = 'me'
  })
  afterEach(cleanup)

  it('lists members and pending invitations with what the viewer may do', async () => {
    mockLists([owner, meMember, editor, sso], [invitation])
    render(<MembersPanel />)

    const list = await screen.findByRole('list', { name: 'Members' })
    expect(api.get).toHaveBeenCalledWith('/members')
    expect(api.get).toHaveBeenCalledWith('/invitations')
    expect(within(list).getByText('Ed')).toBeInTheDocument()
    expect(within(list).getByText('(you)')).toBeInTheDocument()
    expect(within(list).getByText('OIDC')).toBeInTheDocument()
    expect(within(list).getByText('disabled')).toBeInTheDocument()

    // An admin cannot touch the owner, nor remove itself.
    expect(screen.getByLabelText('Role of admin@example.com')).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Remove admin@example.com' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Remove me@example.com' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Remove ed@example.com' })).toBeInTheDocument()
    // and cannot offer owner to anyone.
    expect(within(screen.getByLabelText('Role of ed@example.com')).queryByRole('option', { name: 'owner' })).toBeNull()
    expect(within(screen.getByLabelText('Role')).queryByRole('option', { name: 'owner' })).toBeNull()
    // A disabled account offers Enable instead of Disable.
    expect(screen.getByRole('button', { name: 'Enable' })).toBeInTheDocument()

    const pending = screen.getByRole('list', { name: 'Pending invitations' })
    expect(within(pending).getByText('new@example.com')).toBeInTheDocument()
  })

  it('invites without mail and shows the one-time link to copy', async () => {
    mockLists([meMember])
    api.post.mockResolvedValue({ ...invitation, email_sent: false, invite_url: '/accept-invite?token=abc' })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })

    fireEvent.change(screen.getByLabelText('Email'), { target: { value: ' new@example.com ' } })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'editor' } })
    fireEvent.click(screen.getByRole('button', { name: 'Invite' }))

    const link = await screen.findByLabelText('Invitation link')
    expect(api.post).toHaveBeenCalledWith('/invitations', { email: 'new@example.com', role: 'editor' })
    expect(link).toHaveValue(`${window.location.origin}/accept-invite?token=abc`)
    expect(screen.getByRole('status')).toHaveTextContent(/shown only once/)

    fireEvent.click(screen.getByRole('button', { name: 'Copy link' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/accept-invite?token=abc`))
  })

  it('reports an emailed invitation without a link', async () => {
    mockLists([meMember])
    api.post.mockResolvedValue({ ...invitation, email_sent: true })
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'new@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Invite' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Invitation emailed to new@example.com.')
    expect(screen.queryByLabelText('Invitation link')).toBeNull()
  })

  it('shows quota and permission refusals from the server', async () => {
    mockLists([meMember])
    api.post.mockRejectedValue(new Error('workspace quota max_team_members exceeded'))
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'new@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Invite' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('max_team_members')
  })

  it('creates an account with an initial password', async () => {
    mockLists([meMember])
    api.post.mockResolvedValue(editor)
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    const form = screen.getByRole('form', { name: 'Create account' })
    fireEvent.change(within(form).getByLabelText('Email'), { target: { value: 'ed@example.com' } })
    fireEvent.change(within(form).getByLabelText('Name'), { target: { value: 'Ed' } })
    fireEvent.change(within(form).getByLabelText('Initial password'), { target: { value: 'Initial1234!x' } })
    fireEvent.change(within(form).getByLabelText('Role'), { target: { value: 'editor' } })
    fireEvent.click(within(form).getByRole('button', { name: 'Create account' }))
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith('/members', { email: 'ed@example.com', name: 'Ed', password: 'Initial1234!x', role: 'editor' }),
    )
    expect(await screen.findByRole('status')).toHaveTextContent(/must change the password/)
  })

  it('changes a role, disables and removes through the confirmation dialog', async () => {
    mockLists([meMember, editor])
    api.patch.mockResolvedValue(editor)
    api.delete.mockResolvedValue(undefined)
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })

    fireEvent.change(screen.getByLabelText('Role of ed@example.com'), { target: { value: 'viewer' } })
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('/members/ed-1', { role: 'viewer' }))

    fireEvent.click(screen.getByRole('button', { name: 'Disable ed@example.com' }))
    let dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/signed out everywhere/)).toBeInTheDocument()
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    })
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('/members/ed-1', { disabled: true }))

    fireEvent.click(screen.getByRole('button', { name: 'Remove ed@example.com' }))
    dialog = await screen.findByRole('dialog')
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    })
    await waitFor(() => expect(api.delete).toHaveBeenCalledWith('/members/ed-1'))
  })

  it('surfaces the last-owner refusal', async () => {
    auth.role = 'owner'
    auth.id = 'owner-1'
    mockLists([owner])
    api.patch.mockRejectedValue(new Error('workspace "default" must keep at least one enabled owner'))
    render(<MembersPanel />)
    await screen.findByRole('list', { name: 'Members' })
    fireEvent.change(screen.getByLabelText('Role of admin@example.com'), { target: { value: 'admin' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('at least one enabled owner')
  })

  it('revokes a pending invitation', async () => {
    mockLists([meMember], [invitation])
    api.delete.mockResolvedValue(undefined)
    render(<MembersPanel />)
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke invitation for new@example.com' }))
    const dialog = await screen.findByRole('dialog')
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    })
    await waitFor(() => expect(api.delete).toHaveBeenCalledWith('/invitations/inv-1'))
  })

  it('hides management from roles without the permission', () => {
    auth.role = 'editor'
    render(<MembersPanel />)
    expect(screen.getByText(/only workspace admins and owners/i)).toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
  })

  it('shows load failures', async () => {
    api.get.mockRejectedValue(new Error('boom'))
    render(<MembersPanel />)
    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })
})
