import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dispatchAuthSessionChanged } from '../api/authEvents'
import { Incidents } from './Incidents'

vi.mock('../api/client', () => ({
  api: {
    get: vi.fn().mockResolvedValue([
      { id: 'inc-1', rule_id: 'r', soul_id: 's', soul_name: 'Payments API', status: 'open', severity: 'critical', started_at: '2026-07-06T08:00:00Z' },
    ]),
    post: vi.fn(),
  },
}))

const user = (role: string) => ({ id: 'u', email: 'u@x', name: 'U', role, workspace: 'default' })

// Every role saw acknowledge/resolve and got a 403; the API requires rules:*.
describe('Incidents permissions', () => {
  afterEach(() => act(() => dispatchAuthSessionChanged({ state: 'anonymous' })))

  it.each([
    ['viewer', false],
    ['editor', false],
    ['admin', true],
  ])('%s sees incident actions: %s', async (role, visible) => {
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: user(role) }))
    render(<Incidents />)
    await screen.findByText('Payments API')
    expect(screen.queryAllByRole('button', { name: /acknowledge/i }).length > 0).toBe(visible)
    expect(screen.queryAllByRole('button', { name: /resolve/i }).length > 0).toBe(visible)
  })
})
