import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { WebSocketProvider } from './useWebSocket'
import { useWebSocket } from './webSocketContext'
import { dispatchAuthSessionChanged } from '../api/authEvents'

class GatedSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3
  static instances: GatedSocket[] = []
  readyState = GatedSocket.CONNECTING
  sent: string[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor() { GatedSocket.instances.push(this) }
  send(data: string) { this.sent.push(data) }
  close() { this.readyState = GatedSocket.CLOSED } // Completion explicitly released by each test.
  open() { this.readyState = GatedSocket.OPEN; this.onopen?.() }
  message(type: string) { this.onmessage?.({ data: JSON.stringify({ type, timestamp: 'fixed' }) } as MessageEvent) }
}
function Consumer() {
  const socket = useWebSocket()
  return <>
    <span data-testid="connected">{String(socket.connected)}</span>
    <span data-testid="last">{socket.lastMessage?.type ?? 'none'}</span>
    <button onClick={socket.disconnect}>disconnect</button>
    <button onClick={socket.connect}>connect</button>
    <button onClick={() => socket.send({ type: 'local-test' })}>send</button>
  </>
}
let problem = false
function compare(expected: string, actual: string) {
  console.log(`EXPECTED: ${expected} ACTUAL: ${actual}`)
  if (expected !== actual) { problem = true; console.log('PROBLEM CONFIRMED') }
  expect(actual).toBe(expected)
}
function mountSocket() {
  const view = render(<WebSocketProvider><Consumer /></WebSocketProvider>)
  act(() => dispatchAuthSessionChanged({ state: 'authenticated', user: { id: 'local', email: 'local@example.test', name: 'Local', role: 'admin', workspace: 'default' } }))
  const socket = GatedSocket.instances[0]
  act(() => socket.open())
  return { ...view, socket }
}
function replaceSocket() {
  fireEvent.click(screen.getByText('disconnect'))
  fireEvent.click(screen.getByText('connect'))
  const replacement = GatedSocket.instances[1]
  act(() => replacement.open())
  return replacement
}
describe('Audit socket ownership', () => {
  beforeEach(() => { vi.useFakeTimers(); GatedSocket.instances = []; vi.stubGlobal('WebSocket', GatedSocket) })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })
  afterAll(() => { if (!problem) console.log('FIX VERIFIED') })
  it('control: accepts an active socket message', () => {
    const { socket } = mountSocket()
    act(() => socket.message('status'))
    compare('status', screen.getByTestId('last').textContent ?? '')
  })
  it('ignores old close completion after replacement is open', () => {
    const { socket } = mountSocket()
    const replacement = replaceSocket()
    // Barrier: the replacement opens before the stale close callback is released.
    act(() => socket.onclose?.())
    compare('true', screen.getByTestId('connected').textContent ?? '')
    fireEvent.click(screen.getByText('send'))
    expect(replacement.sent.some(data => JSON.parse(data).type === 'local-test')).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
  it('ignores an old message after replacement', () => {
    const { socket } = mountSocket()
    replaceSocket()
    act(() => socket.message('incident'))
    compare('none', screen.getByTestId('last').textContent ?? '')
  })
  it('ignores an old open completion after disconnect', () => {
    const { socket } = mountSocket()
    fireEvent.click(screen.getByText('disconnect'))
    act(() => socket.open())
    compare('false', screen.getByTestId('connected').textContent ?? '')
  })

  it('ignores open and message callbacks after unmount', () => {
    const { socket, unmount } = mountSocket()
    const sent = socket.sent.length
    unmount()
    act(() => { socket.open(); socket.message('status'); socket.onclose?.() })
    compare(String(sent), String(socket.sent.length))
    expect(vi.getTimerCount()).toBe(0)
  })
  it('still reconnects when the current socket closes', () => {
    const { socket } = mountSocket()
    act(() => socket.onclose?.())
    compare('false', screen.getByTestId('connected').textContent ?? '')
    act(() => vi.advanceTimersByTime(2000))
    expect(GatedSocket.instances).toHaveLength(2)
    act(() => GatedSocket.instances[1].open())
    compare('true', screen.getByTestId('connected').textContent ?? '')
  })

})
