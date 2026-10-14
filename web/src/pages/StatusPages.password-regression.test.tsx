import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { StatusPages } from './StatusPages'

// The API treats a present-but-empty "password" as "clear it", so the editor must
// send the field only when the operator changed or cleared it. These tests pin
// that contract, plus the protected-without-a-password case the API rejects.

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), pages: [] as unknown[], souls: [] }))

vi.mock('../api/hooks', () => ({
  useStatusPages: () => ({
    pages: mocks.pages,
    loading: false,
    createPage: mocks.create,
    updatePage: mocks.update,
    deletePage: vi.fn(),
    refetch: vi.fn(),
  }),
  useSouls: () => ({ souls: mocks.souls }),
}))

afterEach(() => {
  cleanup()
  mocks.create.mockReset()
  mocks.update.mockReset()
  mocks.pages = []
})

async function setup() {
  const ui = render(<StatusPages />)
  await act(async () => {})
  return ui
}

function openCreate() {
  fireEvent.click(screen.getByRole('button', { name: 'Create Page' }))
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Locked page' } })
  fireEvent.change(screen.getByLabelText('Slug'), { target: { value: 'locked-page' } })
}

function selectProtected() {
  fireEvent.change(screen.getByLabelText('Visibility'), { target: { value: 'protected' } })
}

test('a protected page sends its visibility and the password', async () => {
  await setup()
  mocks.create.mockResolvedValue(undefined)

  openCreate()
  selectProtected()
  fireEvent.change(screen.getByLabelText('Page password'), { target: { value: 'page-secret-value' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create Status Page' }))
  await act(async () => {})

  expect(mocks.create).toHaveBeenCalledTimes(1)
  expect(mocks.create.mock.calls[0][0]).toMatchObject({
    visibility: 'protected',
    password: 'page-secret-value',
  })
})

test('a protected page with no password is refused before any request', async () => {
  await setup()
  mocks.create.mockResolvedValue(undefined)

  openCreate()
  selectProtected()
  fireEvent.click(screen.getByRole('button', { name: 'Create Status Page' }))
  await act(async () => {})

  expect(mocks.create).not.toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent('A password-protected page needs a password.')
})

test('the password field shows for a protected page and not for a public one', async () => {
  await setup()
  openCreate()
  expect(screen.queryByLabelText('Page password')).toBeNull()

  selectProtected()
  expect(screen.getByLabelText('Page password')).not.toBeNull()

  fireEvent.change(screen.getByLabelText('Visibility'), { target: { value: 'public' } })
  expect(screen.queryByLabelText('Page password')).toBeNull()
})

test('editing without touching the password omits the field, so the API keeps it', async () => {
  mocks.pages = [{
    id: 'page-1', name: 'Existing', slug: 'existing', enabled: true,
    visibility: 'protected', password_set: true,
  }]
  await setup()
  mocks.update.mockResolvedValue(undefined)

  fireEvent.click(screen.getByRole('button', { name: 'Edit status page Existing' }))
  expect(screen.getByText('A password is set.')).not.toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Save Status Page' }))
  await act(async () => {})

  expect(mocks.update).toHaveBeenCalledTimes(1)
  const payload = mocks.update.mock.calls[0][1]
  expect(payload).toMatchObject({ visibility: 'protected' })
  expect(payload).not.toHaveProperty('password')
})

test('removing the password sends an explicit empty value to clear it', async () => {
  mocks.pages = [{
    id: 'page-1', name: 'Existing', slug: 'existing', enabled: true,
    visibility: 'protected', password_set: true,
  }]
  await setup()
  mocks.update.mockResolvedValue(undefined)

  fireEvent.click(screen.getByRole('button', { name: 'Edit status page Existing' }))
  fireEvent.click(screen.getByRole('button', { name: 'Remove password' }))
  expect(screen.getByText('No password set.')).not.toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Save Status Page' }))
  await act(async () => {})

  expect(mocks.update).toHaveBeenCalledTimes(1)
  expect(mocks.update.mock.calls[0][1]).toMatchObject({ visibility: 'protected', password: '' })
})
