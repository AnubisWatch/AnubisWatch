import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react'

interface WebSocketContextType {
  connected: boolean
  messages: WebSocketMessage[]
  send: (data: unknown) => void
  lastMessage: WebSocketMessage | null
}

interface WebSocketMessage {
  type: string
  data: unknown
  timestamp: number
}

const WebSocketContext = createContext<WebSocketContextType | null>(null)

export function useWebSocket() {
  const context = useContext(WebSocketContext)
  if (!context) {
    throw new Error('useWebSocket must be used within WebSocketProvider')
  }
  return context
}

interface WebSocketProviderProps {
  children: React.ReactNode
}

export function WebSocketProvider({ children }: WebSocketProviderProps) {
  const [connected, setConnected] = useState(false)
  const [messages, setMessages] = useState<WebSocketMessage[]>([])
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const connect = useCallback(() => {
    const wsUrl = `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`

    try {
      const ws = new WebSocket(wsUrl)

      ws.onopen = () => {
        setConnected(true)
        console.log('WebSocket connected')
      }

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as WebSocketMessage
          setMessages((prev) => [...prev.slice(-99), message])
          setLastMessage(message)
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err)
        }
      }

      ws.onclose = () => {
        setConnected(false)
        console.log('WebSocket disconnected, reconnecting...')
        reconnectTimeoutRef.current = setTimeout(connect, 3000)
      }

      ws.onerror = (error) => {
        console.error('WebSocket error:', error)
      }

      wsRef.current = ws
    } catch (err) {
      console.error('Failed to connect WebSocket:', err)
    }
  }, [])

  const send = useCallback((data: unknown) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data))
    }
  }, [])

  useEffect(() => {
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      wsRef.current?.close()
    }
  }, [connect])

  return (
    <WebSocketContext.Provider value={{ connected, messages, send, lastMessage }}>
      {children}
    </WebSocketContext.Provider>
  )
}
