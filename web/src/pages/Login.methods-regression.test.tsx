import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Login } from './Login'

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../api/client', () => ({ api: mocks }))

const renderLogin = () => render(<MemoryRouter><Login /></MemoryRouter>)

describe('Login auth methods', () => {
  beforeEach(() => {
    mocks.get.mockReset()
    mocks.post.mockReset().mockResolvedValue({})
  })

  // /auth/oidc/login existed but the dashboard offered no way to reach it.
  it('shows an SSO button when OIDC is configured', async () => {
    mocks.get.mockResolvedValue({ local: true, oidc: true, ldap: false })
    renderLogin()
    const sso = await screen.findByRole('link', { name: 'Sign in with SSO' })
    expect(sso).toHaveAttribute('href', '/api/v1/auth/oidc/login')
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
  })

  it('falls back to local login when the server lacks /auth/methods', async () => {
    mocks.get.mockRejectedValue(new Error('not found'))
    renderLogin()
    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Sign in with SSO' })).toBeNull()
  })
})
