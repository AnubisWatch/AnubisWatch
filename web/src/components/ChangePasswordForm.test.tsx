import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ChangePasswordForm } from './ChangePasswordForm'

const put = vi.fn()
// Set to make the next PUT fail; a plain rejection keeps vi.fn result tracking out of it.
let serverError = ''
vi.mock('../api/client', () => ({
  api: {
    put: (...args: unknown[]) => {
      put(...args)
      return serverError ? Promise.reject(new Error(serverError)) : Promise.resolve({})
    },
  },
}))

function fill(current: string, next: string, confirm: string) {
  fireEvent.change(screen.getByLabelText('Current password'), { target: { value: current } })
  fireEvent.change(screen.getByLabelText('New password'), { target: { value: next } })
  fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: confirm } })
  fireEvent.click(screen.getByRole('button', { name: 'Change Password' }))
}

// PUT /auth/change-password existed with no UI.
describe('ChangePasswordForm', () => {
  beforeEach(() => {
    put.mockReset()
    serverError = ''
  })

  it('submits the change', async () => {
    render(<ChangePasswordForm />)
    fill('old-Passw0rd!x', 'N3w-Passw0rd!x', 'N3w-Passw0rd!x')
    await waitFor(() => expect(put).toHaveBeenCalledWith('/auth/change-password', { current_password: 'old-Passw0rd!x', new_password: 'N3w-Passw0rd!x' }))
    expect(await screen.findByText('Password changed.')).toBeInTheDocument()
  })

  it('rejects mismatched confirmation locally', async () => {
    render(<ChangePasswordForm />)
    fill('old-Passw0rd!x', 'N3w-Passw0rd!x', 'different-Passw0rd!')
    expect(await screen.findByText('The new passwords do not match.')).toBeInTheDocument()
    expect(put).not.toHaveBeenCalled()
  })

  it('shows server errors', async () => {
    serverError = 'current password is incorrect'
    render(<ChangePasswordForm />)
    fill('bad-Passw0rd!xx', 'N3w-Passw0rd!x', 'N3w-Passw0rd!x')
    expect(await screen.findByText('current password is incorrect')).toBeInTheDocument()
  })
})

describe('ChangePasswordForm onChanged', () => {
  beforeEach(() => {
    put.mockReset()
    serverError = ''
  })

  it('reports a successful change so a forced change can sign out', async () => {
    const onChanged = vi.fn()
    render(<ChangePasswordForm onChanged={onChanged} title="Set your password" />)
    expect(screen.getByRole('heading', { name: 'Set your password' })).toBeInTheDocument()
    fill('Initial1234!x', 'Mine5678!xyz', 'Mine5678!xyz')
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1))
  })

  it('does not report a refused change', async () => {
    serverError = 'current password is incorrect'
    const onChanged = vi.fn()
    render(<ChangePasswordForm onChanged={onChanged} />)
    fill('wrong', 'Mine5678!xyz', 'Mine5678!xyz')
    expect(await screen.findByText('current password is incorrect')).toBeInTheDocument()
    expect(onChanged).not.toHaveBeenCalled()
  })
})
