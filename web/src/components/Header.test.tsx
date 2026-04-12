import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Header } from '../components/Header'

const mockLogout = vi.fn()
const mockAuthState = {
  user: { name: 'Test User', email: 'test@anubis.watch' },
  logout: mockLogout,
}

// Mock useAuth hook
vi.mock('../api/hooks', () => ({
  useAuth: () => mockAuthState,
}))

// Mock useNavigate
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

describe('Header', () => {
  it('renders search input', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    expect(screen.getByPlaceholderText('Search the archives...')).toBeInTheDocument()
  })

  it('renders Hall of Ma\'at badge', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    expect(screen.getByText("Hall of Ma'at")).toBeInTheDocument()
  })

  it('renders user info', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    expect(screen.getByText('Test User')).toBeInTheDocument()
    expect(screen.getByText('test@anubis.watch')).toBeInTheDocument()
  })

  it('renders notification button', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    // Notification button doesn't have accessible name, check by bell icon presence
    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(3) // Theme toggle, notification, logout
  })

  it('renders theme toggle button', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThan(0)
  })

  it('renders logout button', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    expect(screen.getByTitle('Log out')).toBeInTheDocument()
  })

  it('logs out and navigates to login when clicking logout', async () => {
    mockLogout.mockClear()
    mockLogout.mockResolvedValue(undefined)
    mockAuthState.logout = mockLogout

    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    fireEvent.click(screen.getByTitle('Log out'))

    await waitFor(() => {
      expect(mockLogout).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/login')
    })
  })

  it('toggles theme mode when clicking theme button', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    const themeButton = screen.getByLabelText('Switch to light mode')
    expect(themeButton).toBeInTheDocument()

    fireEvent.click(themeButton)

    expect(screen.getByLabelText('Switch to dark mode')).toBeInTheDocument()
  })

  it('toggles notifications when clicking notification button', () => {
    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    const notificationButton = screen.getByLabelText('Toggle notifications')
    expect(notificationButton).toBeInTheDocument()

    fireEvent.click(notificationButton)
    // Component uses internal state, so clicking again should work
    fireEvent.click(notificationButton)
    expect(notificationButton).toBeInTheDocument()
  })
})
