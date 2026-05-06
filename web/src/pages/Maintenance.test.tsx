import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { Maintenance } from './Maintenance'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('../api/client', () => ({
  api: {
    get: mocks.get,
    post: mocks.post,
    put: mocks.put,
    delete: mocks.delete,
  },
}))

describe('Maintenance', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue([])
    mocks.post.mockResolvedValue({})
    mocks.put.mockResolvedValue({})
    mocks.delete.mockResolvedValue({})
  })

  it('creates a maintenance window with ISO timestamps and cleaned tags', async () => {
    render(<Maintenance />)

    await screen.findByText('No maintenance windows')
    fireEvent.click(screen.getByRole('button', { name: /add window/i }))

    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText(/database migration/i), {
      target: { value: 'Database Migration' },
    })
    fireEvent.change(within(dialog).getByPlaceholderText(/what maintenance/i), {
      target: { value: 'Primary DB upgrade' },
    })

    const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]')
    fireEvent.change(timeInputs[0], { target: { value: '2030-01-01T10:00' } })
    fireEvent.change(timeInputs[1], { target: { value: '2030-01-01T12:00' } })
    fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'weekly' } })
    fireEvent.change(within(dialog).getByPlaceholderText(/database, production/i), {
      target: { value: ' database, production, ' },
    })

    fireEvent.click(within(dialog).getByRole('button', { name: /create window/i }))

    await waitFor(() => {
      expect(mocks.post).toHaveBeenCalledWith('/maintenance', {
        name: 'Database Migration',
        description: 'Primary DB upgrade',
        start_time: new Date('2030-01-01T10:00').toISOString(),
        end_time: new Date('2030-01-01T12:00').toISOString(),
        recurring: 'weekly',
        enabled: true,
        tags: ['database', 'production'],
        soul_ids: [],
      })
    })
  })

  it('blocks saving when the end time is not after the start time', async () => {
    render(<Maintenance />)

    await screen.findByText('No maintenance windows')
    fireEvent.click(screen.getByRole('button', { name: /add window/i }))

    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText(/database migration/i), {
      target: { value: 'Bad Window' },
    })
    const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]')
    fireEvent.change(timeInputs[0], { target: { value: '2030-01-01T12:00' } })
    fireEvent.change(timeInputs[1], { target: { value: '2030-01-01T10:00' } })

    fireEvent.click(within(dialog).getByRole('button', { name: /create window/i }))

    expect(await within(dialog).findByText('End time must be after start time')).toBeInTheDocument()
    expect(mocks.post).not.toHaveBeenCalled()
  })
})
