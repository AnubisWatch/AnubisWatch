import { useEffect, useRef, useState, ReactNode, useCallback } from 'react'
import { WebSocketContext, type WebSocketMessage } from './webSocketContext'

interface WebSocketProviderProps {
  children: ReactNode
}

export function WebSocketProvider({ children }: WebSocketProviderProps) {
  const [connected, setConnected] = useState(false)
  const [messages, setMessages] = useState<WebSocketMessage[]>([])
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const shouldReconnectRef = useRef(false)
  const maxReconnectAttempts = 5

  const connect = useCallback(() => {
    // Authenticate via the HttpOnly auth_token cookie (set by server on login).
    // The browser sends cookies automatically with the WebSocket upgrade request.
    // If there's no valid session, the server rejects the connection — the
    // onclose handler will fire and we stop reconnecting.
    shouldReconnectRef.current = true
    if (wsRef.current?.readyState === WebSocket.OPEN || wsRef.current?.readyState === WebSocket.CONNECTING) {
      return
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws`

    try {
      const ws = new WebSocket(wsUrl)

      ws.onopen = () => {
        setConnected(true)
        reconnectAttemptsRef.current = 0

        // Authentication happens during the WebSocket handshake via secure cookie.
        ws.send(JSON.stringify({
          type: 'subscribe',
          events: ['judgment', 'alert', 'incident', 'soul', 'stats', 'status']
        }))
      }

      ws.onmessage = (event) => {
        try {
          const raw = JSON.parse(event.data) as WebSocketMessage
          const message: WebSocketMessage = {
            ...raw,
            data: raw.data ?? raw.payload,
            timestamp: typeof raw.timestamp === 'string' ? raw.timestamp : new Date().toISOString()
          }
          setLastMessage(message)
          setMessages(prev => [...prev.slice(-50), message]) // Keep last 50 messages

          // Handle ping/pong for keepalive
          if (message.type === 'ping') {
            ws.send(JSON.stringify({ type: 'pong', timestamp: new Date().toISOString() }))
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err)
        }
      }

      ws.onclose = () => {
        setConnected(false)
        wsRef.current = null

        if (shouldReconnectRef.current && localStorage.getItem('auth_token') && reconnectAttemptsRef.current < maxReconnectAttempts) {
          reconnectAttemptsRef.current++
          const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 30000)

          reconnectTimeoutRef.current = setTimeout(() => {
            connect()
          }, delay)
        }
      }

      ws.onerror = (error) => {
        console.error('WebSocket error:', error)
      }

      wsRef.current = ws
    } catch (err) {
      console.error('Failed to create WebSocket:', err)
    }
  }, [])

  const disconnect = useCallback(() => {
    shouldReconnectRef.current = false
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }

    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    setConnected(false)
  }, [])

  const send = useCallback((data: unknown) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data))
    } else {
      console.warn('WebSocket not connected')
    }
  }, [])

  // Auto-connect on mount. The HttpOnly cookie (set by server on login) is
  // sent with the WebSocket upgrade request automatically. If the user has
  // a valid session cookie, the connection succeeds; if not, the server
  // rejects it and the onclose handler stops reconnecting.
  useEffect(() => {
    connect()

    return () => {
      disconnect()
    }
  }, [connect, disconnect])

  // Listen for cross-tab storage changes (legacy backward compat).
  // New sessions use HttpOnly cookies, so AUTH_TOKEN_CHANGED_EVENT no longer fires.
  useEffect(() => {
    const handleStorage = (e: StorageEvent) => {
      if (e.key === 'auth_token') {
        if (e.newValue) {
          connect()
        } else {
          disconnect()
        }
      }
    }

    window.addEventListener('storage', handleStorage)
    return () => {
      window.removeEventListener('storage', handleStorage)
    }
  }, [connect, disconnect])

  return (
    <WebSocketContext.Provider value={{
      connected,
      messages,
      send,
      lastMessage,
      connect,
      disconnect
    }}>
      {children}
      {/* Connection Status Indicator */}
      <div className={`fixed bottom-4 right-4 z-50 flex items-center gap-2 px-3 py-2 rounded-full text-xs font-medium transition-all duration-300 ${
        connected
          ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
          : 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
      }`}>
        <span className={`w-2 h-2 rounded-full ${connected ? 'bg-emerald-400 animate-pulse' : 'bg-amber-400'}`} />
        {connected ? 'Live' : 'Offline'}
      </div>
    </WebSocketContext.Provider>
  )
}
