import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, waitFor } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'

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
      this.readyState = MockWebSocket.OPEN
      this.onopen?.()
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

  it('connects on mount and subscribes with backend event names', async () => {
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    // WebSocket connects on mount — no localStorage token needed,
    // the server authenticates via HttpOnly cookie.
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    const ws = MockWebSocket.instances[0]
    expect(ws.url).toBe('ws://localhost:3000/ws')

    await waitFor(() => expect(ws.sent).toHaveLength(1))
    const subscribe = JSON.parse(ws.sent[0])
    expect(subscribe).toEqual({
      type: 'subscribe',
      events: ['judgment', 'alert', 'incident', 'soul', 'stats', 'status']
    })
    expect(subscribe.channels).toBeUndefined()
  })

  it('closes the active socket on unmount', async () => {
    const { unmount } = render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    const ws = MockWebSocket.instances[0]
    expect(ws.closed).toBe(false)

    unmount()

    await waitFor(() => expect(ws.closed).toBe(true))
  })

  it('does not schedule a reconnect after an intentional logout', async () => {
    vi.useFakeTimers()
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    await act(async () => {
      await vi.runOnlyPendingTimersAsync()
    })
    expect(MockWebSocket.instances).toHaveLength(1)

    const ws = MockWebSocket.instances[0]

    // Simulate server-side close (e.g. session expired): close with reason
    act(() => {
      ws.close()
    })

    await vi.advanceTimersByTimeAsync(60_000)

    // After a close without shouldReconnect (no auth token), no reconnect
    expect(MockWebSocket.instances).toHaveLength(1)
    vi.useRealTimers()
  })
})
