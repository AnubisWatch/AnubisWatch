import { createContext, useContext, useEffect, useState, useCallback } from 'react'

const WebSocketContext = createContext(null)

export function WebSocketProvider({ children }) {
  const [ws, setWs] = useState(null)
  const [connected, setConnected] = useState(false)
  const [messages, setMessages] = useState([])

  useEffect(() => {
    const connect = () => {
      const socket = new WebSocket(`ws://${window.location.host}/ws`)

      socket.onopen = () => {
        console.log('WebSocket connected')
        setConnected(true)
        setWs(socket)
      }

      socket.onmessage = (event) => {
        const data = JSON.parse(event.data)
        setMessages((prev) => [...prev.slice(-99), data])
      }

      socket.onclose = () => {
        console.log('WebSocket disconnected')
        setConnected(false)
        setWs(null)
        // Reconnect after 5 seconds
        setTimeout(connect, 5000)
      }

      socket.onerror = (error) => {
        console.error('WebSocket error:', error)
      }
    }

    connect()

    return () => {
      if (ws) {
        ws.close()
      }
    }
  }, [])

  const sendMessage = useCallback((message) => {
    if (ws && connected) {
      ws.send(JSON.stringify(message))
    }
  }, [ws, connected])

  const value = {
    connected,
    messages,
    sendMessage,
    lastMessage: messages[messages.length - 1],
  }

  return (
    <WebSocketContext.Provider value={value}>
      {children}
    </WebSocketContext.Provider>
  )
}

export function useWebSocket() {
  const context = useContext(WebSocketContext)
  if (!context) {
    throw new Error('useWebSocket must be used within a WebSocketProvider')
  }
  return context
}
