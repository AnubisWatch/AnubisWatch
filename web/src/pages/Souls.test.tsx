import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Souls } from './Souls'

const mockFetchSouls = vi.fn()
const mockCreateSoul = vi.fn()
const mockRetryInitialCheck = vi.fn()
const mockUpdateSoul = vi.fn()
const mockDeleteSoul = vi.fn()

vi.mock('../stores/soulStore', () => ({
  useSoulStore: () => ({
    souls: [],
    initialChecks: {},
    fetchSouls: mockFetchSouls,
    createSoul: mockCreateSoul,
    retryInitialCheck: mockRetryInitialCheck,
    updateSoul: mockUpdateSoul,
    deleteSoul: mockDeleteSoul,
  }),
}))

function renderSouls() {
  render(
    <MemoryRouter>
      <Souls />
    </MemoryRouter>
  )
}

describe('Souls create form', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchSouls.mockResolvedValue(undefined)
    mockCreateSoul.mockResolvedValue({
      id: 'soul-1',
      name: 'DNS Check',
      type: 'dns',
      target: 'example.com',
      enabled: true,
      weight: 60,
      timeout: 10,
    })
  })

  it('changes target hints and protocol fields when the soul type changes', () => {
    renderSouls()

    fireEvent.click(screen.getByRole('button', { name: /add soul/i }))
    const dialog = screen.getByRole('dialog')

    expect(within(dialog).getByLabelText('HTTP URL')).toHaveAttribute('placeholder', 'https://api.example.com/health')
    expect(within(dialog).getByLabelText('HTTP Method')).toBeInTheDocument()

    fireEvent.change(within(dialog).getByLabelText('Soul type'), { target: { value: 'dns' } })

    expect(within(dialog).getByLabelText('DNS Name')).toHaveAttribute('placeholder', 'example.com')
    expect(within(dialog).getByLabelText('DNS Record Type')).toBeInTheDocument()
    expect(within(dialog).queryByLabelText('HTTP Method')).not.toBeInTheDocument()

    fireEvent.change(within(dialog).getByLabelText('Soul type'), { target: { value: 'tcp' } })

    expect(within(dialog).getByLabelText('TCP Host and Port')).toHaveAttribute('placeholder', 'api.example.com:443')
    expect(within(dialog).getByLabelText('Expected Banner Regex')).toBeInTheDocument()
    expect(within(dialog).queryByLabelText('DNS Record Type')).not.toBeInTheDocument()
  })

  it('submits a DNS soul with DNS-specific config instead of HTTP config', async () => {
    renderSouls()

    fireEvent.click(screen.getByRole('button', { name: /add soul/i }))
    const dialog = screen.getByRole('dialog')

    fireEvent.change(within(dialog).getByPlaceholderText('e.g., Production API'), { target: { value: 'DNS Check' } })
    fireEvent.change(within(dialog).getByLabelText('Soul type'), { target: { value: 'dns' } })
    fireEvent.change(within(dialog).getByLabelText('DNS Name'), { target: { value: 'example.com' } })
    fireEvent.change(within(dialog).getByLabelText('DNS Record Type'), { target: { value: 'AAAA' } })
    fireEvent.change(within(dialog).getByLabelText('Expected DNS Values'), { target: { value: '2001:db8::1' } })

    fireEvent.click(within(dialog).getByRole('button', { name: /create soul/i }))

    await waitFor(() => {
      expect(mockCreateSoul).toHaveBeenCalledWith(expect.objectContaining({
        name: 'DNS Check',
        type: 'dns',
        target: 'example.com',
        dns: {
          record_type: 'AAAA',
          expected: ['2001:db8::1'],
        },
      }))
    })

    expect(mockCreateSoul.mock.calls[0][0]).not.toHaveProperty('http')
  })
})
