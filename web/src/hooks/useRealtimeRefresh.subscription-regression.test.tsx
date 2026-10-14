import { expect, it, vi } from 'vitest'
import { act, render } from '@testing-library/react'
import { WebSocketContext, type WebSocketContextType, type WebSocketMessage } from './webSocketContext'
import { useRealtimeRefresh } from './useRealtimeRefresh'
function Harness({ types, refetch }: { types: readonly WebSocketMessage['type'][]; refetch: () => void }) { useRealtimeRefresh(types, refetch, 10); return null }
const context: WebSocketContextType = { connected: true, messages: [], lastMessage: { type: 'status', timestamp: 'fixed' }, send() {}, connect() {}, disconnect() {} }
it('audit pending refresh subscription ownership', () => {
 vi.useFakeTimers()
 try {
  const cases: {name: string; types: readonly WebSocketMessage['type'][]; want: number; unrelated?: boolean; unmount?: boolean; resubscribe?: boolean}[] = [
   { name: 'unchanged-control', types: ['status'], want: 1 },
   { name: 'unsubscribe', types: [], want: 0 },
   { name: 'switch-event', types: ['incident'], want: 0 },
   { name: 'unrelated-event-control', types: ['status'], unrelated: true, want: 1 },
   { name: 'unmount', types: ['status'], unmount: true, want: 0 },
   { name: 'resubscribe', types: [], resubscribe: true, want: 1 },
  ]
  for (const tc of cases) {
   const refetch = vi.fn()
   const node = (types: readonly WebSocketMessage['type'][], value = context) => <WebSocketContext.Provider value={value}><Harness types={types} refetch={refetch} /></WebSocketContext.Provider>
   const view = render(node(['status']))
   expect(refetch).not.toHaveBeenCalled()
   view.rerender(node(tc.types, tc.unrelated ? { ...context, lastMessage: { type: 'stats', timestamp: 'next' } } : context))
   if (tc.resubscribe) view.rerender(node(['status']))
   if (tc.unmount) view.unmount()
   act(() => { vi.advanceTimersByTime(10) })
   console.log(`${tc.name} EXPECTED: calls=${tc.want} ACTUAL: calls=${refetch.mock.calls.length}`)
   if (refetch.mock.calls.length !== tc.want) console.log('regression mismatch')
   expect(refetch).toHaveBeenCalledTimes(tc.want)
   view.unmount()
   expect(vi.getTimerCount()).toBe(0)
  }
 } finally { vi.clearAllTimers(); vi.useRealTimers() }
})
