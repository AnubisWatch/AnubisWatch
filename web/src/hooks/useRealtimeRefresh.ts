import { useEffect, useRef } from 'react'
import { useWebSocket, type WebSocketMessage } from './webSocketContext'

/**
 * Re-fetches data when a relevant WebSocket event arrives, so lists and stats
 * update live instead of only after a manual action. Events are debounced:
 * a burst of judgments triggers a single refetch, not one per message.
 *
 * Outside a WebSocketProvider (e.g. isolated component tests) the context
 * default never changes, so this is a no-op.
 */
export function useRealtimeRefresh(
  eventTypes: readonly WebSocketMessage['type'][],
  refetch: () => void | Promise<unknown>,
  debounceMs = 750
) {
  const { lastMessage } = useWebSocket()
  const refetchRef = useRef(refetch)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const typesKey = eventTypes.join(',')

  useEffect(() => {
    refetchRef.current = refetch
  }, [refetch])

  useEffect(() => {
    if (!lastMessage || !typesKey.split(',').includes(lastMessage.type)) return
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => {
      timerRef.current = null
      Promise.resolve(refetchRef.current()).catch(() => {
        // The owning hook exposes fetch errors through its own error state.
      })
    }, debounceMs)
  }, [lastMessage, typesKey, debounceMs])

  useEffect(() => () => {
    if (timerRef.current) clearTimeout(timerRef.current)
  }, [])
}
