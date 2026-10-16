import { useSyncExternalStore } from 'react'
import { AUTH_SESSION_CHANGED_EVENT, getLastKnownUser } from '../api/authEvents'

// Mirrors MemberRole.Can in internal/core/workspace.go. The server enforces
// these; the dashboard only uses them to hide controls that would 403.
const rolePermissions: Record<string, string[] | '*'> = {
  owner: '*',
  admin: ['souls:*', 'channels:*', 'rules:*', 'members:*', 'settings:read', 'settings:write'],
  editor: ['souls:*', 'channels:read', 'rules:read'],
  viewer: ['souls:read', 'judgments:read', 'channels:read', 'rules:read'],
  api: ['souls:*', 'judgments:read', 'api:*'],
}

export type Permission =
  | 'souls:write'
  | 'channels:write'
  | 'rules:write'
  | 'members:write'
  | 'settings:write'

export function roleCan(role: string | undefined, permission: string): boolean {
  const perms = rolePermissions[role ?? '']
  if (perms === '*') return true
  if (!perms) return false
  return perms.some((p) => p === '*' || p === permission || (p.endsWith(':*') && permission.startsWith(p.slice(0, -1))))
}

function subscribe(onChange: () => void) {
  window.addEventListener(AUTH_SESSION_CHANGED_EVENT, onChange)
  return () => window.removeEventListener(AUTH_SESSION_CHANGED_EVENT, onChange)
}

const currentRole = () => getLastKnownUser()?.role

/**
 * Returns a predicate for the signed-in user's role. Until the session is
 * known every control stays visible, so nothing flickers away on load.
 */
export function useCan(): (permission: Permission) => boolean {
  const role = useSyncExternalStore(subscribe, currentRole, currentRole)
  return (permission) => role === undefined || roleCan(role, permission)
}
