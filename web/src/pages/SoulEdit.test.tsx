import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route, useNavigate, useParams } from 'react-router-dom'
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
})
