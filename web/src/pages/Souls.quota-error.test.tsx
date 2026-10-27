import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { Souls } from './Souls'

const mockCreateSoul = vi.fn()

vi.mock('../stores/soulStore', () => ({
  useSoulStore: () => ({
    souls: [],
    initialChecks: {},
    fetchSouls: vi.fn().mockResolvedValue(undefined),
    createSoul: mockCreateSoul,
    retryInitialCheck: vi.fn(),
    updateSoul: vi.fn(),
    deleteSoul: vi.fn(),
  }),
}))

describe('Souls create quota refusal', () => {
  beforeEach(() => {
    mockCreateSoul.mockReset()
    Object.defineProperty(window, 'alert', { value: vi.fn(), configurable: true })
  })
  afterEach(cleanup)

  it('shows the quota error inside the still-open modal instead of an alert', async () => {
    mockCreateSoul.mockRejectedValueOnce(
      new ApiError(409, { error: 'workspace quota max_souls exceeded', code: 'quota_exceeded', quota: 'max_souls', limit: 2, current: 2 }),
    )
    render(
      <MemoryRouter>
        <Souls />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: /add soul/i }))
    fireEvent.change(screen.getByPlaceholderText('e.g., Production API'), { target: { value: 'API' } })
    fireEvent.change(screen.getByTestId('soul-target'), { target: { value: 'https://example.com' } })
    fireEvent.click(screen.getByRole('button', { name: /create soul/i }))

    const banner = await screen.findByRole('alert')
    expect(banner).toHaveTextContent('Quota reached: this workspace may hold at most 2 souls.')
    expect(banner).toHaveTextContent('tenants.default_quotas.max_souls')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(window.alert).not.toHaveBeenCalled()

    // Retrying clears the banner before the request goes out again.
    mockCreateSoul.mockResolvedValueOnce({ id: 's1' })
    fireEvent.click(screen.getByRole('button', { name: /create soul/i }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})
