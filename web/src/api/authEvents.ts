import type { User } from './client'

export const AUTH_SESSION_CHANGED_EVENT = 'anubis:auth-session-changed'
// Kept as an alias for consumers that still import the old event name.
export const AUTH_TOKEN_CHANGED_EVENT = AUTH_SESSION_CHANGED_EVENT

export type AuthSessionChange =
  | { state: 'resync' }
  | { state: 'authenticated'; user: User }
  | { state: 'anonymous' }

export function dispatchAuthSessionChanged(change: AuthSessionChange) {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(
      new CustomEvent<AuthSessionChange>(AUTH_SESSION_CHANGED_EVENT, {
        detail: change,
      }),
    )
  }
}

export function dispatchAuthTokenChanged() {
  dispatchAuthSessionChanged({ state: 'resync' })
}
