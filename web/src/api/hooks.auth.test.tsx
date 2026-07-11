import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from './hooks'
import { dispatchAuthSessionChanged } from './authEvents'
import type { User } from './client'

const user: User = {
  id: 'user-1',
  email: 'admin@anubis.watch',
  name: 'Admin',
  role: 'admin',
  workspace: 'default',
}

const mockGet = vi.fn()
const mockPost = vi.fn()
const mockClearToken = vi.fn()

vi.mock('./client', async () => {
  const actual = await vi.importActual<typeof import('./client')>('./client')
  return {
    ...actual,
    api: {
      get: (...args: unknown[]) => mockGet(...args),
      post: (...args: unknown[]) => mockPost(...args),
      clearToken: (...args: unknown[]) => mockClearToken(...args),
    },
  }
})

describe('useAuth cookie session synchronization', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mockGet.mockResolvedValue(user)
    mockPost.mockResolvedValue(undefined)
  })

  it('resyncs every mounted instance and rejects a cached user when /auth/me fails', async () => {
    localStorage.setItem('auth_user', JSON.stringify(user))
    const first = renderHook(() => useAuth())
    const second = renderHook(() => useAuth())

    await waitFor(() => expect(first.result.current.user).toEqual(user))
    await waitFor(() => expect(second.result.current.user).toEqual(user))

    mockGet.mockRejectedValue(new Error('network unavailable'))
    act(() => dispatchAuthSessionChanged({ state: 'resync' }))

    await waitFor(() => expect(first.result.current.loading).toBe(false))
    expect(first.result.current.isAuthenticated).toBe(false)
    expect(second.result.current.isAuthenticated).toBe(false)
  })

  it('propagates logout immediately before the server request settles', async () => {
    let resolveLogout: (() => void) | undefined
    mockPost.mockImplementation(
      () => new Promise<void>((resolve) => { resolveLogout = resolve }),
    )
    const first = renderHook(() => useAuth())
    const second = renderHook(() => useAuth())
    await waitFor(() => expect(first.result.current.isAuthenticated).toBe(true))
    await waitFor(() => expect(second.result.current.isAuthenticated).toBe(true))

    let logoutPromise: Promise<void>
    act(() => {
      logoutPromise = first.result.current.logout()
    })

    expect(first.result.current.isAuthenticated).toBe(false)
    expect(second.result.current.isAuthenticated).toBe(false)
    expect(mockClearToken).toHaveBeenCalledWith(false)

    resolveLogout?.()
    await act(async () => logoutPromise!)
  })
})
