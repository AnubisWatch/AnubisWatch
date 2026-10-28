import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { Journeys } from './Journeys'
import { dispatchAuthSessionChanged } from '../api/authEvents'

vi.mock('../api/client', () => ({ api: { get: vi.fn(async () => [{
  id: 'permission-journey', name: 'Permission Journey', enabled: true,
  weight: 60, timeout: 10, step_count: 1, last_status: 'passed', avg_duration: 1, success_rate: 100,
}]) } }))

afterEach(() => {
  cleanup()
  dispatchAuthSessionChanged({ state: 'anonymous' })
})

test.each(['viewer', 'editor', 'owner'])('%s sees only permitted journey controls', async (role) => {
  dispatchAuthSessionChanged({ state: 'authenticated', user: {
    id: 'fixture', email: 'fixture@example.test', name: 'Fixture', role, workspace: 'default',
  } })
  render(<Journeys />)
  await screen.findByText('Permission Journey')
  const edit = screen.getByLabelText('Edit journey Permission Journey')
  if (role === 'viewer') expect(edit).not.toBeVisible()
  else expect(edit).toBeVisible()
  expect(edit).not.toHaveTextContent('hidden=')
})
