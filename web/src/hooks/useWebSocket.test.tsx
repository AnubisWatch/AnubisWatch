import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, waitFor } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'
import { api } from '../api/client'

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

  it('connects on same-tab auth token changes and subscribes with backend event names', async () => {
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    expect(MockWebSocket.instances).toHaveLength(0)

    act(() => {
      api.setToken('test-token')
    })

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

  it('closes the active socket when the auth token is cleared in the same tab', async () => {
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    act(() => {
      api.setToken('test-token')
    })
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    const ws = MockWebSocket.instances[0]

    act(() => {
      api.clearToken()
    })

    await waitFor(() => expect(ws.closed).toBe(true))
  })

  it('does not schedule a reconnect after an intentional logout', async () => {
    vi.useFakeTimers()
    render(
      <WebSocketProvider>
        <div>content</div>
      </WebSocketProvider>
    )

    act(() => {
      api.setToken('test-token')
    })
    await act(async () => {
      await vi.runOnlyPendingTimersAsync()
    })
    expect(MockWebSocket.instances).toHaveLength(1)

    act(() => {
      api.clearToken()
    })
    await act(async () => {
      await vi.runOnlyPendingTimersAsync()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })

    expect(MockWebSocket.instances).toHaveLength(1)
  })
})
