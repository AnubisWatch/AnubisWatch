import { act, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, expect, it, vi } from 'vitest'
import { BarChartWidget } from './BarChartWidget'
const state = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: state.post } }))
vi.mock('recharts', () => ({ ResponsiveContainer: ({ children }: { children: ReactNode }) => children, BarChart: ({ data }: { data: object[] }) => <div data-testid="chart-data">{JSON.stringify(data)}</div>, Bar: () => null, Cell: () => null, XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null }))
const widget = (metric: string) => ({ id: 'w1', title: 'Fixture', type: 'bar_chart' as const, grid: { x: 0, y: 0, width: 1, height: 1 }, query: { source: 'judgments', metric, time_range: '1h' } })
function deferred() { let resolve!: (value: unknown) => void; let reject!: (error: Error) => void; const promise = new Promise((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
beforeEach(() => state.post.mockReset())
it('current zero bar data remains data', async () => {
  state.post.mockResolvedValue([{ passed: 0, failed: 0 }])
  await act(async () => { render(<BarChartWidget widget={widget('current')} dashboardId="A" />) })
  expect(screen.getByTestId('chart-data')).toHaveTextContent('[{"passed":0,"failed":0}]')
})
it.each(['same dashboard', 'new dashboard'])('failed replacement clears previous bars: %s', async mode => {
  const pending = deferred(); state.post.mockResolvedValueOnce([{ passed: 99 }]).mockReturnValueOnce(pending.promise)
  let view!: ReturnType<typeof render>
  await act(async () => { view = render(<BarChartWidget widget={widget('old')} dashboardId="A" />) })
  view.rerender(<BarChartWidget widget={widget('current')} dashboardId={mode === 'same dashboard' ? 'A' : 'B'} />)
  expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  await act(async () => { pending.reject(new Error('New failure')) })
  expect(screen.getByText('No data')).toBeInTheDocument()
  expect(screen.queryByTestId('chart-data')).not.toBeInTheDocument()
})
it('stale failure cannot erase current bars', async () => {
  const old = deferred(); state.post.mockReturnValueOnce(old.promise).mockResolvedValueOnce([{ passed: 22 }])
  const view = render(<BarChartWidget widget={widget('old')} dashboardId="A" />)
  await act(async () => { view.rerender(<BarChartWidget widget={widget('current')} dashboardId="B" />) })
  await act(async () => { old.reject(new Error('Old failure')) })
  expect(screen.getByTestId('chart-data')).toHaveTextContent('22')
})
it('stale success cannot clear current pending loading', async () => {
  const old = deferred(); const current = deferred()
  state.post.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
  const view = render(<BarChartWidget widget={widget('old')} dashboardId="A" />)
  view.rerender(<BarChartWidget widget={widget('current')} dashboardId="B" />)
  await act(async () => { old.resolve([{ passed: 99 }]) })
  expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  await act(async () => { current.resolve([{ passed: 22 }]) })
  expect(screen.getByTestId('chart-data')).toHaveTextContent('22')
})
