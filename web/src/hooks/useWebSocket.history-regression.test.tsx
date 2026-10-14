import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'
import { useWebSocket } from './webSocketContext'
import { dispatchAuthSessionChanged } from '../api/authEvents'
class HistorySocket {
  static CONNECTING = 0
  static OPEN = 1
  static instances: HistorySocket[] = []
  readyState = HistorySocket.OPEN
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor() { HistorySocket.instances.push(this) }
  send() {}
  close() {}
}
function Consumer() {
  const { messages, lastMessage } = useWebSocket()
  return <><span data-testid="count">{messages.length}</span><span data-testid="first">{String(messages[0]?.data ?? 'empty')}</span><span data-testid="last">{String(lastMessage?.data ?? 'empty')}</span></>
}
function compare(expected: string, actual: string) {
  console.log(`EXPECTED: ${expected} ACTUAL: ${actual}`)
  if (expected !== actual) { console.log('Regression mismatch') }
  expect(actual).toBe(expected)
}
describe('Audit message history limit', () => {
  beforeEach(() => { HistorySocket.instances = []; vi.stubGlobal('WebSocket', HistorySocket) })
  afterEach(() => vi.unstubAllGlobals())
  it.each([50, 51, 0, 1, 49, 100])('retains latest messages after %i events', (count) => {
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: { id: 'local', email: 'local@example.test', name: 'Local', role: 'admin', workspace: 'default' } }))
    const socket = HistorySocket.instances[0]
    act(() => { socket.onopen?.(); for (let i = 0; i < count; i++) socket.onmessage?.({ data: JSON.stringify({ type: 'status', data: i, timestamp: 'fixed' }) } as MessageEvent) })
    compare(String(Math.min(count, 50)), screen.getByTestId('count').textContent ?? '')
    compare(count ? String(Math.max(0, count - 50)) : 'empty', screen.getByTestId('first').textContent ?? '')
    compare(count ? String(count - 1) : 'empty', screen.getByTestId('last').textContent ?? '')
  })
})
