import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { dispatchAuthSessionChanged } from '../api/authEvents'
import { roleCan, useCan } from './permissions'

describe('roleCan mirrors the backend role table', () => {
  it.each([
    ['owner', 'settings:write', true],
    ['admin', 'channels:write', true],
    ['admin', 'members:write', true],
    ['editor', 'souls:write', true],
    ['editor', 'rules:write', false],
    ['editor', 'settings:write', false],
    ['viewer', 'souls:write', false],
    ['viewer', 'souls:read', true],
    ['api', 'souls:write', true],
    ['stranger', 'souls:read', false],
  ])('%s %s → %s', (role, perm, want) => {
    expect(roleCan(role, perm)).toBe(want)
  })
})

describe('useCan', () => {
  afterEach(() => dispatchAuthSessionChanged({ state: 'anonymous' }))

  // Viewers (the LDAP/OIDC default) used to see every mutating control and get a 403.
  it('follows the signed-in role', () => {
    const { result } = renderHook(() => useCan())
    expect(result.current('settings:write')).toBe(true) // unknown session: show
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: { id: '1', email: 'v@x', name: 'V', role: 'viewer', workspace: 'default' } }))
    expect(result.current('souls:write')).toBe(false)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: { id: '1', email: 'a@x', name: 'A', role: 'admin', workspace: 'default' } }))
    expect(result.current('souls:write')).toBe(true)
  })
})
