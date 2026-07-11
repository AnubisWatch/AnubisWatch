import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'
import { useWebSocket } from './webSocketContext'
import { dispatchAuthSessionChanged } from '../api/authEvents'
import type { User } from '../api/client'

const user: User = {
  id: 'user-1',
  email: 'admin@anubis.watch',
  name: 'Admin',
  role: 'admin',
  workspace: 'default',
}

function Consumer() {
  const socket = useWebSocket()
  return <>
    <span data-testid="count">{socket.messages.length}</span>
    <span data-testid="last">{socket.lastMessage?.type ?? 'none'}</span>
    <button onClick={() => socket.send({ hello: 'world' })}>send</button>
    <button onClick={socket.connect}>connect</button>
    <button onClick={socket.disconnect}>disconnect</button>
  </>
}

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3
  static instances: MockWebSocket[] = []

  readyState = MockWebSocket.CONNECTING
  sent: string[] = []
  closed = false
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null

  constructor(public url: string) {
    MockWebSocket.instances.push(this)
    setTimeout(() => {
      if (!this.closed) {
        this.readyState = MockWebSocket.OPEN
        this.onopen?.()
      }
    }, 0)
  }

  send(message: string) {
    this.sent.push(message)
  }

  close() {
    this.closed = true
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }
}

describe('WebSocketProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('waits for an authenticated session and subscribes without a localStorage token', async () => {
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    expect(MockWebSocket.instances).toHaveLength(0)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))

    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    const ws = MockWebSocket.instances[0]
    expect(ws.url).toBe('ws://localhost:3000/ws')

    await waitFor(() => expect(ws.sent).toHaveLength(1))
    expect(JSON.parse(ws.sent[0])).toEqual({
      type: 'subscribe',
      events: ['judgment', 'alert', 'incident', 'soul', 'stats', 'status']
    })
  })

  it('closes the active socket on logout and does not reconnect', async () => {
    vi.useFakeTimers()
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    await act(async () => {
      await vi.runOnlyPendingTimersAsync()
    })
    const ws = MockWebSocket.instances[0]

    act(() => dispatchAuthSessionChanged({ state: 'anonymous' }))
    expect(ws.closed).toBe(true)

    await vi.advanceTimersByTimeAsync(60_000)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('exposes harmless context defaults without a provider', () => {
    render(<Consumer />)
    fireEvent.click(screen.getByText('send'))
    fireEvent.click(screen.getByText('connect'))
    fireEvent.click(screen.getByText('disconnect'))
    expect(screen.getByTestId('count')).toHaveTextContent('0')
  })

  it('normalizes messages, responds to pings, sends data, and reports parse/socket errors', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    fireEvent.click(screen.getByText('send'))
    expect(warn).toHaveBeenCalledWith('WebSocket not connected')
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    const ws = MockWebSocket.instances[0]
    await waitFor(() => expect(ws.readyState).toBe(MockWebSocket.OPEN))
    act(() => ws.onmessage?.({ data: JSON.stringify({ type: 'ping', payload: { ok: true }, timestamp: 1 }) } as MessageEvent))
    expect(screen.getByTestId('last')).toHaveTextContent('ping')
    expect(ws.sent.some((message) => JSON.parse(message).type === 'pong')).toBe(true)
    fireEvent.click(screen.getByText('send'))
    expect(ws.sent.some((message) => JSON.parse(message).hello === 'world')).toBe(true)
    act(() => ws.onmessage?.({ data: '{' } as MessageEvent))
    act(() => ws.onerror?.(new Event('error')))
    expect(error).toHaveBeenCalledTimes(2)
    fireEvent.click(screen.getByText('disconnect'))
  })

  it('does not create duplicate sockets while connecting and ignores connect before auth', async () => {
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    fireEvent.click(screen.getByText('connect'))
    expect(MockWebSocket.instances).toHaveLength(0)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    fireEvent.click(screen.getByText('connect'))
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('uses wss on HTTPS and defaults missing data to undefined', async () => {
    const original = window.location
    Object.defineProperty(window, 'location', { configurable: true, value: { ...original, protocol: 'https:', host: 'secure.test' } })
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    expect(MockWebSocket.instances[0].url).toBe('wss://secure.test/ws')
    act(() => MockWebSocket.instances[0].onmessage?.({ data: JSON.stringify({ type: 'status', timestamp: 'now' }) } as MessageEvent))
    expect(screen.getByTestId('last')).toHaveTextContent('status')
    Object.defineProperty(window, 'location', { configurable: true, value: original })
  })

  it('ignores unknown session events', () => {
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    act(() => window.dispatchEvent(new CustomEvent('anubis:auth-session-changed', { detail: { state: 'other' } })))
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('handles constructor failures and secure websocket URLs', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('WebSocket', class { static OPEN = 1; static CONNECTING = 0; constructor() { throw new Error('boom') } })
    render(<WebSocketProvider><Consumer /></WebSocketProvider>)
    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    expect(error).toHaveBeenCalledWith('Failed to create WebSocket:', expect.any(Error))
  })

  it('reconnects after an unexpected close only while the session is active', async () => {
    vi.useFakeTimers()
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    act(() => dispatchAuthSessionChanged({ state: 'authenticated', user }))
    await act(async () => {
      await vi.runOnlyPendingTimersAsync()
    })
    act(() => MockWebSocket.instances[0].close())

    await vi.advanceTimersByTimeAsync(2_000)
    expect(MockWebSocket.instances).toHaveLength(2)
  })
})
