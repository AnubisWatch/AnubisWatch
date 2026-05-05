export const AUTH_TOKEN_CHANGED_EVENT = 'anubis:auth-token-changed'

export function dispatchAuthTokenChanged() {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(AUTH_TOKEN_CHANGED_EVENT))
  }
}
