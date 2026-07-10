import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Login } from './Login'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

const mockSetToken = vi.fn()
const mockPost = vi.fn()
vi.mock('../api/client', () => ({
  api: {
    post: (...args: unknown[]) => mockPost(...args as [string, unknown?]),
    setToken: (token: string) => mockSetToken(token),
  },
}))

describe('Login', () => {
  beforeEach(() => {
    mockNavigate.mockClear()
    mockSetToken.mockClear()
    mockPost.mockClear()
  })

  it('renders login form', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )

    expect(screen.getByPlaceholderText('priest@anubis.watch')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('••••••••')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /enter the temple/i })).toBeInTheDocument()
    expect(screen.getByText('Use the admin credentials configured for this instance.')).toBeInTheDocument()
  })

  it('toggles password visibility', () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )

    const passwordInput = screen.getByPlaceholderText('••••••••') as HTMLInputElement
    const toggleButton = screen.getByLabelText('Show password')

    expect(passwordInput.type).toBe('password')

    fireEvent.click(toggleButton)
    expect(passwordInput.type).toBe('text')

    fireEvent.click(screen.getByLabelText('Hide password'))
    expect(passwordInput.type).toBe('password')
  })

  it('submits form and navigates on success', async () => {
    mockPost.mockResolvedValue({ user: { id: '1', email: 'test@example.com', name: 'Test' } })

    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )

    fireEvent.change(screen.getByPlaceholderText('priest@anubis.watch'), {
      target: { value: 'test@example.com' },
    })
    fireEvent.change(screen.getByPlaceholderText('••••••••'), {
      target: { value: 'password' },
    })

    fireEvent.click(screen.getByRole('button', { name: /enter the temple/i }))

    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/auth/login', { email: 'test@example.com', password: 'password' })
    })
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/')
    })
  })

  it('shows error message on login failure', async () => {
    mockPost.mockRejectedValue(new Error('Invalid credentials'))

    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )

    fireEvent.change(screen.getByPlaceholderText('priest@anubis.watch'), {
      target: { value: 'test@example.com' },
    })
    fireEvent.change(screen.getByPlaceholderText('••••••••'), {
      target: { value: 'wrong' },
    })

    fireEvent.click(screen.getByRole('button', { name: /enter the temple/i }))

    await waitFor(() => {
      expect(screen.getByText('Invalid credentials')).toBeInTheDocument()
    })
  })

  it('shows generic error for non-Error rejections', async () => {
    mockPost.mockRejectedValue('unknown error')

    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    )

    fireEvent.change(screen.getByPlaceholderText('priest@anubis.watch'), {
      target: { value: 'test@example.com' },
    })
    fireEvent.change(screen.getByPlaceholderText('••••••••'), {
      target: { value: 'wrong' },
    })

    fireEvent.click(screen.getByRole('button', { name: /enter the temple/i }))

    await waitFor(() => {
      expect(screen.getByText('The gods have rejected your offering')).toBeInTheDocument()
    })
  })
})
