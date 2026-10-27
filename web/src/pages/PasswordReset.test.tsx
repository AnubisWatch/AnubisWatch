import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Login } from './Login'
import { AcceptInvite, ForgotPassword, ResetPassword } from './PasswordReset'

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../api/client', () => ({ api: mocks }))

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname + location.search}</div>
}

describe('Forgot password link', () => {
  beforeEach(() => {
    mocks.get.mockReset()
    mocks.post.mockReset().mockResolvedValue({})
  })

  it('is shown when the server can email reset links', async () => {
    mocks.get.mockResolvedValue({ local: true, oidc: false, ldap: false, password_reset: true })
    render(<MemoryRouter><Login /></MemoryRouter>)
    const link = await screen.findByRole('link', { name: 'Forgot password?' })
    expect(link).toHaveAttribute('href', '/forgot-password')
  })

  it('is hidden without outgoing mail', async () => {
    mocks.get.mockResolvedValue({ local: true, oidc: false, ldap: false, password_reset: false })
    render(<MemoryRouter><Login /></MemoryRouter>)
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith('/auth/methods'))
    await screen.findByLabelText('Email')
    expect(screen.queryByRole('link', { name: 'Forgot password?' })).toBeNull()
  })

  it('is hidden for servers that do not report the flag', async () => {
    mocks.get.mockRejectedValue(new Error('not found'))
    render(<MemoryRouter><Login /></MemoryRouter>)
    await screen.findByLabelText('Email')
    expect(screen.queryByRole('link', { name: 'Forgot password?' })).toBeNull()
  })
})

describe('ForgotPassword', () => {
  beforeEach(() => {
    mocks.post.mockReset().mockResolvedValue({ message: 'sent' })
  })

  it('requests a reset email and shows a neutral confirmation', async () => {
    render(<MemoryRouter><ForgotPassword /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'admin@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send reset link' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/If that address belongs to an account/)
    expect(mocks.post).toHaveBeenCalledWith('/auth/reset-password', { email: 'admin@example.com' })
  })

  it('shows an error when the request fails', async () => {
    mocks.post.mockRejectedValue(new Error('rate limited'))
    render(<MemoryRouter><ForgotPassword /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'a@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send reset link' }))
    expect(await screen.findByText('rate limited')).toBeInTheDocument()
  })
})

describe('ResetPassword', () => {
  beforeEach(() => {
    mocks.post.mockReset().mockResolvedValue({})
  })

  const renderAt = (url: string) =>
    render(
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/reset-password" element={<><ResetPassword /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )

  it('submits the emailed token and drops it from the address bar', async () => {
    renderAt('/reset-password?token=tok-123')
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(/^\/reset-password$/))
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'N3w-Passw0rd!x' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'N3w-Passw0rd!x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Set new password' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/has been reset/)
    expect(mocks.post).toHaveBeenCalledWith('/auth/reset-password/confirm', {
      token: 'tok-123',
      new_password: 'N3w-Passw0rd!x',
    })
  })

  it('refuses mismatched passwords without calling the server', async () => {
    renderAt('/reset-password?token=tok-123')
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'N3w-Passw0rd!x' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'Different-Passw0rd!' } })
    fireEvent.click(screen.getByRole('button', { name: 'Set new password' }))
    expect(await screen.findByText('The passwords do not match.')).toBeInTheDocument()
    expect(mocks.post).not.toHaveBeenCalled()
  })

  it('reports an invalid or expired token', async () => {
    mocks.post.mockRejectedValue(new Error('reset token expired'))
    renderAt('/reset-password?token=old')
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'N3w-Passw0rd!x' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'N3w-Passw0rd!x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Set new password' }))
    expect(await screen.findByText('reset token expired')).toBeInTheDocument()
  })

  it('explains a link without a token', () => {
    renderAt('/reset-password')
    expect(screen.getByText(/link is missing or incomplete/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Set new password' })).toBeNull()
  })
})

describe('AcceptInvite', () => {
  beforeEach(() => {
    mocks.post.mockReset().mockResolvedValue({})
  })

  const renderAt = (url: string) =>
    render(
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/accept-invite" element={<><AcceptInvite /><LocationProbe /></>} />
        </Routes>
      </MemoryRouter>,
    )

  const fillIn = (name: string, password: string, confirm: string) => {
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: name } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: password } })
    fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: confirm } })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))
  }

  it('redeems the token with a name and password and drops it from the address bar', async () => {
    renderAt('/accept-invite?token=inv-123')
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(/^\/accept-invite$/))
    fillIn(' New Person ', 'N3w-Passw0rd!x', 'N3w-Passw0rd!x')
    expect(await screen.findByRole('status')).toHaveTextContent(/Invitation accepted/)
    expect(mocks.post).toHaveBeenCalledWith('/auth/accept-invite', {
      token: 'inv-123',
      name: 'New Person',
      password: 'N3w-Passw0rd!x',
    })
  })

  it('refuses mismatched passwords without calling the server', async () => {
    renderAt('/accept-invite?token=inv-123')
    fillIn('New Person', 'N3w-Passw0rd!x', 'Other-Passw0rd!x')
    expect(await screen.findByText('The passwords do not match.')).toBeInTheDocument()
    expect(mocks.post).not.toHaveBeenCalled()
  })

  it('reports a used or expired invitation', async () => {
    mocks.post.mockRejectedValue(new Error('invalid or expired invitation'))
    renderAt('/accept-invite?token=used')
    fillIn('New Person', 'N3w-Passw0rd!x', 'N3w-Passw0rd!x')
    expect(await screen.findByText('invalid or expired invitation')).toBeInTheDocument()
  })

  it('explains a link without a token', () => {
    renderAt('/accept-invite')
    expect(screen.getByText(/link is missing or incomplete/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Create account' })).toBeNull()
  })
})
