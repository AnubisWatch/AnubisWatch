import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { SoulEdit } from './SoulEdit'
import React from 'react'

// Mock the hooks module
const mockUpdateSoul = vi.fn()
const mockUseSoul = vi.fn()

vi.mock('../api/hooks', () => ({
  useSoul: () => mockUseSoul()
}))

// Override useNavigate and useParams
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await import('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useParams: () => ({ id: 'test-soul-id' })
  }
})

describe('SoulEdit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading state with spinner', () => {
    mockUseSoul.mockReturnValue({
      soul: null,
      loading: true,
      error: null,
      updateSoul: mockUpdateSoul
    })

    render(
      <MemoryRouter initialEntries={['/souls/test-soul-id/edit']}>
        <Routes>
          <Route path="/souls/:id/edit" element={<SoulEdit />} />
        </Routes>
      </MemoryRouter>
    )

    // Loading should show a spinner, not "Edit Soul"
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('renders error state', () => {
    mockUseSoul.mockReturnValue({
      soul: null,
      loading: false,
      error: 'Soul not found',
      updateSoul: mockUpdateSoul
    })

    render(
      <MemoryRouter initialEntries={['/souls/test-soul-id/edit']}>
        <Routes>
          <Route path="/souls/:id/edit" element={<SoulEdit />} />
        </Routes>
      </MemoryRouter>
    )

    expect(screen.getByText('Soul not found')).toBeInTheDocument()
  })

  it('renders form with soul data', () => {
    mockUseSoul.mockReturnValue({
      soul: {
        id: 'test-soul-id',
        name: 'Test Soul',
        type: 'http',
        target: 'https://example.com',
        enabled: true,
        weight: 60,
        timeout: 10,
        tags: ['production']
      },
      loading: false,
      error: null,
      updateSoul: mockUpdateSoul
    })

    render(
      <MemoryRouter initialEntries={['/souls/test-soul-id/edit']}>
        <Routes>
          <Route path="/souls/:id/edit" element={<SoulEdit />} />
        </Routes>
      </MemoryRouter>
    )

    expect(screen.getByText('Edit Soul')).toBeInTheDocument()
  })

  it('calls updateSoul and navigates on save', async () => {
    mockUpdateSoul.mockResolvedValue({ id: 'test-soul-id' })
    mockUseSoul.mockReturnValue({
      soul: {
        id: 'test-soul-id',
        name: 'Test Soul',
        type: 'http',
        target: 'https://example.com',
        enabled: true,
        weight: 60,
        timeout: 10,
        tags: []
      },
      loading: false,
      error: null,
      updateSoul: mockUpdateSoul
    })

    render(
      <MemoryRouter initialEntries={['/souls/test-soul-id/edit']}>
        <Routes>
          <Route path="/souls/:id/edit" element={<SoulEdit />} />
        </Routes>
      </MemoryRouter>
    )

    const saveButton = screen.getByRole('button', { name: /save changes/i })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(mockUpdateSoul).toHaveBeenCalled()
      expect(mockNavigate).toHaveBeenCalledWith('/souls/test-soul-id')
    })
  })

  it('navigates back on cancel', () => {
    mockUseSoul.mockReturnValue({
      soul: {
        id: 'test-soul-id',
        name: 'Test Soul',
        type: 'http',
        target: 'https://example.com',
        enabled: true,
        weight: 60,
        timeout: 10,
        tags: []
      },
      loading: false,
      error: null,
      updateSoul: mockUpdateSoul
    })

    render(
      <MemoryRouter initialEntries={['/souls/test-soul-id/edit']}>
        <Routes>
          <Route path="/souls/:id/edit" element={<SoulEdit />} />
        </Routes>
      </MemoryRouter>
    )

    const cancelButton = screen.getByRole('button', { name: /cancel/i })
    fireEvent.click(cancelButton)

    expect(mockNavigate).toHaveBeenCalledWith('/souls/test-soul-id')
  })

  it('handles the missing-soul fallback and its back action', () => {
    mockUseSoul.mockReturnValue({ soul: null, loading: false, error: null, updateSoul: mockUpdateSoul })
    render(<MemoryRouter><SoulEdit /></MemoryRouter>)
    expect(screen.getByText('Soul not found')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Back to Souls' }))
    expect(mockNavigate).toHaveBeenCalledWith('/souls')
  })

  it('updates all general fields, uses numeric fallbacks, and supports the header back action', () => {
    mockUseSoul.mockReturnValue({
      soul: { id: 'test-soul-id', name: 'Test Soul', type: 'http', target: 'https://example.com', enabled: true, weight: 60, timeout: 10, tags: [] },
      loading: false, error: null, updateSoul: mockUpdateSoul
    })
    const { container } = render(<MemoryRouter><SoulEdit /></MemoryRouter>)
    const form = container.querySelector('form')!
    const textInputs = form.querySelectorAll<HTMLInputElement>('input[type="text"]')
    const numberInputs = form.querySelectorAll<HTMLInputElement>('input[type="number"]')
    fireEvent.change(textInputs[0], { target: { value: 'Renamed Soul' } })
    fireEvent.change(screen.getByLabelText('Soul type'), { target: { value: 'tcp' } })
    fireEvent.change(container.querySelector('#edit-soul-target')!, { target: { value: 'host:443' } })
    fireEvent.change(numberInputs[numberInputs.length - 2], { target: { value: '' } })
    fireEvent.change(numberInputs[numberInputs.length - 1], { target: { value: '' } })
    fireEvent.click(screen.getByLabelText('Enable monitoring'))
    fireEvent.click(container.querySelector('button')!)
    expect(screen.getByDisplayValue('Renamed Soul')).toBeInTheDocument()
    expect(screen.getByDisplayValue('60')).toBeInTheDocument()
    expect(screen.getByDisplayValue('10')).toBeInTheDocument()
    expect(mockNavigate).toHaveBeenCalledWith('/souls/test-soul-id')
  })

  it.each([
    [new Error('save exploded'), 'save exploded'],
    ['not an error', 'Failed to save'],
  ])('shows save failures without navigating', async (reason, message) => {
    mockUpdateSoul.mockRejectedValueOnce(reason)
    mockUseSoul.mockReturnValue({
      soul: { id: 'test-soul-id', name: 'Test Soul', type: 'http', target: 'https://example.com', enabled: true, weight: 60, timeout: 10, tags: [] },
      loading: false, error: null, updateSoul: mockUpdateSoul
    })
    render(<MemoryRouter><SoulEdit /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /save changes/i }))
    expect(await screen.findByText(message)).toBeInTheDocument()
    expect(mockNavigate).not.toHaveBeenCalled()
  })
})
