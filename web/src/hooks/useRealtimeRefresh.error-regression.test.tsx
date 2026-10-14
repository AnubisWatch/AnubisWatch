import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render } from '@testing-library/react'
import { WebSocketContext, type WebSocketContextType } from './webSocketContext'
import { useRealtimeRefresh } from './useRealtimeRefresh'
function Harness({ refetch }: { refetch: () => void | Promise<unknown> }) { useRealtimeRefresh(['status'], refetch, 1); return null }
const context: WebSocketContextType = { connected: true, messages: [], lastMessage: { type: 'status', timestamp: 'fixed' }, send() {}, connect() {}, disconnect() {} }
async function flush() {
  let escaped = false
  await act(async () => { try { vi.advanceTimersByTime(1) } catch { escaped = true }; await Promise.resolve() })
  console.log(`EXPECTED: escaped error=false ACTUAL: escaped error=${escaped}`)
  if (escaped) { console.log('Regression mismatch') }
  expect(escaped).toBe(false)
}
function mount(refetch: () => void | Promise<unknown>) { return render(<WebSocketContext.Provider value={context}><Harness refetch={refetch} /></WebSocketContext.Provider>) }
describe('Audit realtime callback errors', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })
  it('control: consumes a rejected promise', async () => { const refetch = vi.fn(() => Promise.reject(new Error('local fixture failure'))); mount(refetch); await flush(); expect(refetch).toHaveBeenCalledTimes(1) })
  it('consumes a synchronous throw', async () => { const refetch = vi.fn(() => { throw new Error('local fixture failure') }); mount(refetch); await flush(); expect(refetch).toHaveBeenCalledTimes(1) })
  it('still refreshes after a callback fails', async () => {
    let calls = 0
    const refetch = () => { calls++; if (calls === 1) throw new Error('first callback failure') }
    const view = mount(refetch); await flush()
    view.rerender(<WebSocketContext.Provider value={{ ...context, lastMessage: { type: 'status', timestamp: 'next' } }}><Harness refetch={refetch} /></WebSocketContext.Provider>)
    await flush(); expect(calls).toBe(2)
  })
  it('uses the latest callback while debounce is pending', async () => {
    const old = vi.fn(() => { throw new Error('obsolete callback') }); const latest = vi.fn()
    const view = mount(old)
    view.rerender(<WebSocketContext.Provider value={context}><Harness refetch={latest} /></WebSocketContext.Provider>)
    await flush(); expect(old).not.toHaveBeenCalled(); expect(latest).toHaveBeenCalledTimes(1)
  })
  it('cancels a throwing callback on unmount', async () => {
    const refetch = vi.fn(() => { throw new Error('disposed callback') }); const view = mount(refetch); view.unmount()
    await flush(); expect(refetch).not.toHaveBeenCalled()
  })
})
