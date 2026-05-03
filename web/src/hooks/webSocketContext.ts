import { createContext, useContext } from 'react'

export interface WebSocketMessage {
  type: 'connected' | 'judgment' | 'alert' | 'incident' | 'soul_update' | 'stats' | 'status' | 'success' | 'error' | 'ping' | 'pong'
  data?: unknown
  payload?: unknown
  timestamp: string
}

export interface WebSocketContextType {
  connected: boolean
  messages: WebSocketMessage[]
  send: (data: unknown) => void
  lastMessage: WebSocketMessage | null
  connect: () => void
  disconnect: () => void
}

export const WebSocketContext = createContext<WebSocketContextType>({
  connected: false,
  messages: [],
  send: () => {},
  lastMessage: null,
  connect: () => {},
  disconnect: () => {}
})

export function useWebSocket() {
  return useContext(WebSocketContext)
}
