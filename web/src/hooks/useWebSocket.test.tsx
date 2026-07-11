import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, waitFor } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'
import { dispatchAuthSessionChanged } from '../api/authEvents'
import type { User } from '../api/client'

const user: User = {
  id: 'user-1',
  email: 'admin@anubis.watch',
  name: 'Admin',
  role: 'admin',
  workspace: 'default',
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
