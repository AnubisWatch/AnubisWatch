import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { Journeys } from './Journeys'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('../api/client', () => ({
  api: {
    get: apiMocks.get,
    post: apiMocks.post,
    put: apiMocks.put,
    delete: apiMocks.delete,
  },
}))

describe('Journeys', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.get.mockResolvedValue([
      {
        id: 'journey-1',
        name: 'Checkout Flow',
        description: 'Critical purchase path',
        enabled: true,
        weight: 60,
        timeout: 30,
        step_count: 1,
        last_status: 'passed',
        avg_duration: 1200,
        success_rate: 99,
        steps: [
          {
            name: 'Open checkout',
            type: 'http',
            target: 'https://example.com/checkout',
            timeout: 10,
            assertions: [{ type: 'status_code', target: '', operator: 'equals', expected: '200' }],
          },
        ],
        continue_on_failure: false,
      },
    ])
    apiMocks.post.mockResolvedValue({})
    apiMocks.put.mockResolvedValue({})
    apiMocks.delete.mockResolvedValue({})
  })

  it('opens the edit modal with journey values and saves through update', async () => {
    render(<Journeys />)

    await screen.findByText('Checkout Flow')
    fireEvent.click(screen.getByRole('button', { name: /edit journey checkout flow/i }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Checkout Flow')
    expect(within(dialog).getByLabelText('Interval (s)')).toHaveValue(60)
    expect(within(dialog).getByDisplayValue('Open checkout')).toBeInTheDocument()

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Checkout Flow Updated' } })
    fireEvent.click(within(dialog).getByRole('button', { name: /save journey/i }))

    await waitFor(() => {
      expect(apiMocks.put).toHaveBeenCalledWith('/journeys/journey-1', expect.objectContaining({
        name: 'Checkout Flow Updated',
        enabled: true,
        weight: 60,
        timeout: 30,
        step_count: 1,
      }))
    })
    expect(apiMocks.post).not.toHaveBeenCalled()
  })
})
