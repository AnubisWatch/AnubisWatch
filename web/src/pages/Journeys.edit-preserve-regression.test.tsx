import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Journeys } from './Journeys'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('../api/client', () => ({ api: apiMocks }))

const login = {
  id: 'journey-login',
  name: 'Login Flow',
  enabled: true,
  weight: 60,
  timeout: 30,
  step_count: 2,
  last_status: 'passed',
  avg_duration: 100,
  success_rate: 100,
  variables: { base: 'https://example.com' },
  steps: [
    {
      name: 'Sign in',
      type: 'http',
      target: '${base}/login',
      timeout: 10,
      http: { method: 'POST', body: '{"user":"a"}', valid_status: [200], headers: { 'Content-Type': 'application/json' } },
      extract: { token: { from: 'body', path: '$.token' } },
      assertions: [],
    },
    { name: 'Profile', type: 'http', target: '${base}/me', timeout: 10, assertions: [] },
  ],
}

describe('Journeys edit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.get.mockResolvedValue([login])
    apiMocks.put.mockResolvedValue({})
  })

  // Editing a journey used to drop variables, step extraction and the step's
  // http block (the serializer then defaulted it to GET).
  it('keeps variables, extraction and step protocol config on save', async () => {
    render(<Journeys />)
    await screen.findByText('Login Flow')
    fireEvent.click(screen.getByRole('button', { name: 'Edit journey Login Flow' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Login Flow v2' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save Journey' }))

    await waitFor(() => expect(apiMocks.put).toHaveBeenCalled())
    const [, body] = apiMocks.put.mock.calls[0]
    expect(body.name).toBe('Login Flow v2')
    expect(body.variables).toEqual({ base: 'https://example.com' })
    expect(body.steps[0].http).toMatchObject({ method: 'POST', body: '{"user":"a"}' })
    expect(body.steps[0].extract).toEqual({ token: { from: 'body', path: '$.token' } })
    expect(body.steps[0]).not.toHaveProperty('original')
    expect(body.steps[1].target).toBe('${base}/me')
  })
})
