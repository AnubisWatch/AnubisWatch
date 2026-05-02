import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Header } from '../components/Header'

// Use hoisted mocks so they are available before the vi.mock() call
const mockLogout = vi.hoisted(() => vi.fn())
const mockSetTheme = vi.hoisted(() => vi.fn())
const mockApplyTheme = vi.hoisted(() => vi.fn())
const mockGetEffectiveTheme = vi.hoisted(() => vi.fn(() => 'dark'))

const mockAuthState = {
  user: { name: 'Test User', email: 'test@anubis.watch' },
  logout: mockLogout,
}

// Mock useAuth hook
vi.mock('../api/hooks', () => ({
  useAuth: () => mockAuthState,
}))

// Mock themeStore
vi.mock('../stores/themeStore', () => ({
  useThemeStore: () => ({
    theme: 'dark',
    setTheme: mockSetTheme,
  }),
  applyTheme: mockApplyTheme,
  getEffectiveTheme: mockGetEffectiveTheme,
}))

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

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(3)
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

  it('toggles theme when clicking theme button', async () => {
    mockSetTheme.mockClear()
    mockApplyTheme.mockClear()

    render(
      <MemoryRouter>
        <Header />
      </MemoryRouter>
    )

    const themeButton = screen.getByLabelText('Switch to light mode')
    expect(themeButton).toBeInTheDocument()

    fireEvent.click(themeButton)

    await waitFor(() => {
      expect(mockSetTheme).toHaveBeenCalledWith('light')
    })
    expect(mockApplyTheme).toHaveBeenCalledWith('light')
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
    fireEvent.click(notificationButton)
  })
})
