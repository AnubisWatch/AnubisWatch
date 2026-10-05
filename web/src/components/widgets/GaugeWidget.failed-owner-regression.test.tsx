import { act, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { GaugeWidget } from './GaugeWidget'
const state = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: state.post } }))
const widget = (metric: string) => ({ id: 'w1', title: `Gauge ${metric}`, type: 'gauge' as const, grid: { x: 0, y: 0, width: 1, height: 1 }, query: { source: 'stats', metric, time_range: '1h' } })
function deferred() { let resolve!: (value: unknown) => void; let reject!: (error: Error) => void; const promise = new Promise((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
beforeEach(() => state.post.mockReset())
it('current zero measurement remains zero', async () => {
  state.post.mockResolvedValue({ current: 0 })
  await act(async () => { render(<GaugeWidget widget={widget('current')} dashboardId="A" />) })
  expect(screen.getByText('0.0%')).toBeInTheDocument()
})
it.each(['same dashboard', 'new dashboard'])('failed replacement cannot retain old data: %s', async mode => {
  const pending = deferred(); state.post.mockResolvedValueOnce({ old: 99 }).mockReturnValueOnce(pending.promise)
  let view!: ReturnType<typeof render>
  await act(async () => { view = render(<GaugeWidget widget={widget('old')} dashboardId="A" />) })
  view.rerender(<GaugeWidget widget={widget('current')} dashboardId={mode === 'same dashboard' ? 'A' : 'B'} />)
  expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  await act(async () => { pending.reject(new Error('New failure')) })
  expect(screen.getByText('Gauge current')).toBeInTheDocument()
  expect(screen.queryByText('99.0%')).not.toBeInTheDocument()
  expect(screen.getByText('0.0%')).toBeInTheDocument()
})
it('stale failure cannot clear replacement success', async () => {
  const old = deferred(); state.post.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ current: 22 })
  const view = render(<GaugeWidget widget={widget('old')} dashboardId="A" />)
  await act(async () => { view.rerender(<GaugeWidget widget={widget('current')} dashboardId="B" />) })
  await act(async () => { old.reject(new Error('Old failure')) })
  expect(screen.getByText('22.0%')).toBeInTheDocument()
})
it('replaced success cannot clear latest pending loading', async () => {
  const old = deferred(); const current = deferred()
  state.post.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
  const view = render(<GaugeWidget widget={widget('old')} dashboardId="A" />)
  view.rerender(<GaugeWidget widget={widget('current')} dashboardId="B" />)
  await act(async () => { old.resolve({ old: 99 }) })
  expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  await act(async () => { current.resolve({ current: 22 }) })
  expect(screen.getByText('22.0%')).toBeInTheDocument()
})
