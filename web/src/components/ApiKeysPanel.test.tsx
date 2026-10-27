import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiKeysPanel } from './ApiKeysPanel'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api }))

const auth = vi.hoisted(() => ({ role: 'admin' as string | undefined }))
vi.mock('../api/authEvents', () => ({
  AUTH_SESSION_CHANGED_EVENT: 'anubis:auth-session-changed',
  getLastKnownUser: () => (auth.role ? { id: 'u1', role: auth.role } : null),
}))

const activeKey = {
  id: 'aaaaaaaaaaaaaaaaaaaaaaaa',
  name: 'deploy bot',
  prefix: 'anb_aaaaaaaaaaaaaaaaaaaaaaaa',
  workspace_id: 'default',
  owner_id: 'u1',
  role: 'editor',
  status: 'active',
  created_at: '2026-10-01T00:00:00Z',
  expires_at: '2027-01-01T00:00:00Z',
}
const revokedKey = { ...activeKey, id: 'bbbbbbbbbbbbbbbbbbbbbbbb', name: 'old', status: 'revoked', expires_at: undefined }

describe('ApiKeysPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.role = 'admin'
  })
  afterEach(cleanup)

  it('lists keys without offering to revoke revoked ones', async () => {
    api.get.mockResolvedValue([activeKey, revokedKey])
    render(<ApiKeysPanel />)

    const list = await screen.findByRole('list', { name: 'API keys' })
    expect(api.get).toHaveBeenCalledWith('/api-keys')
    expect(within(list).getByText('deploy bot')).toBeInTheDocument()
    expect(within(list).getByText('revoked')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Revoke deploy bot' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Revoke old' })).not.toBeInTheDocument()
  })

  it('creates a key, reveals it once and copies it', async () => {
    api.get.mockResolvedValue([])
    api.post.mockResolvedValue({ ...activeKey, key: 'anb_aaaaaaaaaaaaaaaaaaaaaaaa_secret' })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(<ApiKeysPanel />)
    await screen.findByText('No API keys yet.')

    // An admin cannot pick owner.
    const roleSelect = screen.getByLabelText('Role')
    expect(within(roleSelect).queryByRole('option', { name: 'owner' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: ' deploy bot ' } })
    fireEvent.change(roleSelect, { target: { value: 'editor' } })
    fireEvent.change(screen.getByLabelText('Expires'), { target: { value: '30' } })
    fireEvent.click(screen.getByRole('button', { name: /create api key/i }))

    const reveal = await screen.findByLabelText('New API key')
    expect(api.post).toHaveBeenCalledWith('/api-keys', { name: 'deploy bot', role: 'editor', expires_in_days: 30 })
    expect(reveal).toHaveValue('anb_aaaaaaaaaaaaaaaaaaaaaaaa_secret')
    expect(screen.getByText(/will not be shown again/i)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('anb_aaaaaaaaaaaaaaaaaaaaaaaa_secret'))
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.queryByLabelText('New API key')).not.toBeInTheDocument()
  })

  it('requires a name and shows server errors', async () => {
    api.get.mockResolvedValue([])
    api.post.mockRejectedValue(new Error('a key with role "owner" would exceed your role "admin"'))
    render(<ApiKeysPanel />)
    await screen.findByText('No API keys yet.')

    fireEvent.click(screen.getByRole('button', { name: /create api key/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Name is required')
    expect(api.post).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: /create api key/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent('would exceed your role')
  })

  it('revokes through the confirmation dialog', async () => {
    api.get.mockResolvedValueOnce([activeKey]).mockResolvedValue([{ ...activeKey, status: 'revoked' }])
    api.delete.mockResolvedValue(undefined)
    render(<ApiKeysPanel />)

    fireEvent.click(await screen.findByRole('button', { name: 'Revoke deploy bot' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/stops working immediately/i)).toBeInTheDocument()
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    })

    await waitFor(() => expect(api.delete).toHaveBeenCalledWith('/api-keys/aaaaaaaaaaaaaaaaaaaaaaaa'))
    expect(await screen.findByText('revoked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Revoke deploy bot' })).not.toBeInTheDocument()
  })

  it('cancelling the dialog keeps the key', async () => {
    api.get.mockResolvedValue([activeKey])
    render(<ApiKeysPanel />)
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke deploy bot' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(api.delete).not.toHaveBeenCalled()
  })

  it('shows load failures', async () => {
    api.get.mockRejectedValue(new Error('boom'))
    render(<ApiKeysPanel />)
    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })

  it('hides management from roles without the permission', async () => {
    auth.role = 'viewer'
    render(<ApiKeysPanel />)
    expect(screen.getByText(/only workspace admins and owners/i)).toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: /create api key/i })).not.toBeInTheDocument()
  })

  it('lets owners mint owner keys', async () => {
    auth.role = 'owner'
    api.get.mockResolvedValue([])
    render(<ApiKeysPanel />)
    await screen.findByText('No API keys yet.')
    expect(within(screen.getByLabelText('Role')).getByRole('option', { name: 'owner' })).toBeInTheDocument()
  })
})
