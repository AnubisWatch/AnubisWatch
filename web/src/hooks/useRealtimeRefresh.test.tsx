import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render } from '@testing-library/react'
import { act } from 'react'
import {
  WebSocketContext,
  type WebSocketContextType,
  type WebSocketMessage,
} from './webSocketContext'
import { useRealtimeRefresh } from './useRealtimeRefresh'

function makeContext(lastMessage: WebSocketMessage | null): WebSocketContextType {
  return {
    connected: true,
    messages: [],
    send: () => {},
    lastMessage,
    connect: () => {},
    disconnect: () => {},
  }
}

function makeMessage(type: WebSocketMessage['type']): WebSocketMessage {
  return { type, timestamp: new Date().toISOString() }
}

interface HarnessProps {
  types: readonly WebSocketMessage['type'][]
  refetch: () => void | Promise<unknown>
  debounceMs?: number
}

function Harness({ types, refetch, debounceMs }: HarnessProps) {
  useRealtimeRefresh(types, refetch, debounceMs)
  return null
}

function renderWithMessage(
  props: HarnessProps,
  lastMessage: WebSocketMessage | null
) {
  const view = render(
    <WebSocketContext.Provider value={makeContext(lastMessage)}>
      <Harness {...props} />
    </WebSocketContext.Provider>
  )
  const setMessage = (message: WebSocketMessage | null) => {
    view.rerender(
      <WebSocketContext.Provider value={makeContext(message)}>
        <Harness {...props} />
      </WebSocketContext.Provider>
    )
  }
  return { ...view, setMessage }
}

describe('useRealtimeRefresh', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does nothing without a message', () => {
    const refetch = vi.fn()
    renderWithMessage({ types: ['judgment'], refetch }, null)

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(refetch).not.toHaveBeenCalled()
  })

  it('refetches after the debounce delay on a matching event', () => {
    const refetch = vi.fn()
    const { setMessage } = renderWithMessage({ types: ['judgment'], refetch }, null)

    act(() => {
      setMessage(makeMessage('judgment'))
    })
    expect(refetch).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(750)
    })
    expect(refetch).toHaveBeenCalledTimes(1)
  })

  it('ignores events that are not subscribed', () => {
    const refetch = vi.fn()
    const { setMessage } = renderWithMessage({ types: ['incident'], refetch }, null)

    act(() => {
      setMessage(makeMessage('judgment'))
    })
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(refetch).not.toHaveBeenCalled()
  })

  it('collapses a burst of events into a single refetch', () => {
    const refetch = vi.fn()
    const { setMessage } = renderWithMessage({ types: ['judgment'], refetch }, null)

    act(() => {
      setMessage(makeMessage('judgment'))
    })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    act(() => {
      setMessage(makeMessage('judgment'))
    })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    expect(refetch).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(750)
    })
    expect(refetch).toHaveBeenCalledTimes(1)
  })

  it('honors a custom debounce delay', () => {
    const refetch = vi.fn()
    const { setMessage } = renderWithMessage(
      { types: ['stats'], refetch, debounceMs: 100 },
      null
    )

    act(() => {
      setMessage(makeMessage('stats'))
    })
    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(refetch).toHaveBeenCalledTimes(1)
  })

  it('cancels the pending refetch on unmount', () => {
    const refetch = vi.fn()
    const { setMessage, unmount } = renderWithMessage(
      { types: ['judgment'], refetch },
      null
    )

    act(() => {
      setMessage(makeMessage('judgment'))
    })
    unmount()
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(refetch).not.toHaveBeenCalled()
  })

  it('swallows refetch rejections', async () => {
    const refetch = vi.fn(() => Promise.reject(new Error('network down')))
    const { setMessage } = renderWithMessage({ types: ['judgment'], refetch }, null)

    act(() => {
      setMessage(makeMessage('judgment'))
    })
    await act(async () => {
      vi.advanceTimersByTime(750)
      await Promise.resolve()
    })
    expect(refetch).toHaveBeenCalledTimes(1)
  })
})
