import { act, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { WorkspaceSwitcher } from './WorkspaceSwitcher'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
const user = { id: 'u1', email: 'test@example.test', name: 'User', role: 'admin', workspace: 'default' }
it.each([
  { reason: new Error('Workspace fixture unavailable'), expected: 'Workspace fixture unavailable' },
  { reason: 'offline', expected: 'Failed to load workspaces' },
])('reports an inaccessible load error: $expected', async ({ reason, expected }) => {
  api.get.mockRejectedValue(reason)
  await act(async () => { render(<WorkspaceSwitcher user={user} />) })
  expect(screen.getByRole('button', { name: 'Switch workspace' })).toBeDisabled()
  expect(screen.getByRole('alert')).toHaveTextContent(expected)
  expect(screen.queryByRole('menu')).not.toBeInTheDocument()
})
it('clears a prior load error after a successful reload for new user props', async () => {
  api.get.mockRejectedValueOnce(new Error('Temporary fixture failure')).mockResolvedValueOnce([{ id: 'default', name: 'Default' }, { id: 'ops', name: 'Operations' }])
  let view!: ReturnType<typeof render>
  await act(async () => { view = render(<WorkspaceSwitcher user={user} />) })
  expect(screen.getByRole('alert')).toHaveTextContent('Temporary fixture failure')
  await act(async () => { view.rerender(<WorkspaceSwitcher user={{ ...user }} />) })
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Switch workspace' })).toBeEnabled()
})
it('empty successful workspace list keeps the trigger disabled without an error', async () => {
  api.get.mockResolvedValue([])
  await act(async () => { render(<WorkspaceSwitcher user={user} />) })
  expect(screen.getByRole('button', { name: 'Switch workspace' })).toBeDisabled()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})
