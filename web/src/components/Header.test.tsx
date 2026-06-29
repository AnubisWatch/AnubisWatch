import { describe, it, expect, vi } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
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
  it('renders search input', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    expect(screen.getByPlaceholderText('Search the archives...')).toBeInTheDocument()
  })

  it('renders Hall of Ma\'at badge', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    expect(screen.getByText("Hall of Ma'at")).toBeInTheDocument()
  })

  it('renders user info', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    expect(screen.getByText('Test User')).toBeInTheDocument()
    expect(screen.getByText('test@anubis.watch')).toBeInTheDocument()
  })

  it('renders notification button', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(3)
  })

  it('renders theme toggle button', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThan(0)
  })

  it('renders logout button', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    expect(screen.getByTitle('Log out')).toBeInTheDocument()
  })

  it('toggles theme when clicking theme button', async () => {
    mockSetTheme.mockClear()
    mockApplyTheme.mockClear()

    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    const themeButton = screen.getByLabelText('Switch to light mode')
    expect(themeButton).toBeInTheDocument()

    await act(async () => {
      fireEvent.click(themeButton)
    })

    await waitFor(() => {
      expect(mockSetTheme).toHaveBeenCalledWith('light')
    })
    expect(mockApplyTheme).toHaveBeenCalledWith('light')
  })

  it('toggles notifications when clicking notification button', async () => {
    await act(async () => {
      render(
        <MemoryRouter>
          <Header />
        </MemoryRouter>
      )
    })

    const notificationButton = screen.getByLabelText('Toggle notifications')
    expect(notificationButton).toBeInTheDocument()

    await act(async () => {
      fireEvent.click(notificationButton)
    })
    await act(async () => {
      fireEvent.click(notificationButton)
    })
  })
})
