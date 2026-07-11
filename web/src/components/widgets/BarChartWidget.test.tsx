import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { BarChartWidget } from './BarChartWidget'
import type { WidgetConfig } from '../../api/client'

const mocks = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: mocks.post } }))

const makeWidget = (overrides?: Partial<WidgetConfig>): WidgetConfig => ({
  id: 'w1',
  title: 'Errors',
  type: 'bar_chart',
  grid: { x: 0, y: 0, width: 2, height: 1 },
  query: { source: 'judgments', metric: 'errors', time_range: '1h' },
  ...overrides,
})

describe('BarChartWidget', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('shows loading spinner initially', () => {
    mocks.post.mockReturnValue(new Promise(() => {}))
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('ignores a response that settles after unmount', async () => {
    let resolve!: (value: Array<Record<string, unknown>>) => void
    mocks.post.mockReturnValue(new Promise((done) => { resolve = done }))
    const view = render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    view.unmount()
    resolve([{ passed: 1 }])
    await Promise.resolve()
  })

  it('renders pass/fail chart data', async () => {
    mocks.post.mockResolvedValue([{ time: '10:00', passed: 5, failed: 1 }])
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })

  it('renders chart with data', async () => {
    mocks.post.mockResolvedValue([
      { time: '10:00', count: 5, avg_latency: 120 },
      { time: '11:00', count: 8, avg_latency: 95 },
    ])
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })

  it('shows no data when empty response', async () => {
    mocks.post.mockResolvedValue([])
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('No data')).toBeInTheDocument())
  })

  it('shows no data on API error', async () => {
    mocks.post.mockRejectedValue(new Error('API error'))
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('No data')).toBeInTheDocument())
  })

  it('uses avg_latency key for avg aggregation', async () => {
    mocks.post.mockResolvedValue([{ time: '10:00', count: 5, avg_latency: 120 }])
    render(
      <BarChartWidget
        widget={makeWidget({ query: { source: 'judgments', metric: 'latency', time_range: '1h', aggregation: 'avg' } })}
        dashboardId="d1"
      />
    )
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })

  it('handles an entry that disappears before key-value extraction', async () => {
    const changing: Array<Record<string, unknown>> = []
    changing.length = 1
    let reads = 0
    Object.defineProperty(changing, 0, { get: () => (++reads % 3 === 0 ? undefined : {}) })
    mocks.post.mockResolvedValue(changing)
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })

  it('handles a null object response as no data', async () => {
    mocks.post.mockResolvedValue(null)
    render(<BarChartWidget widget={makeWidget()} dashboardId="d1" />)
    expect(await screen.findByText('No data')).toBeInTheDocument()
  })

  it('renders key-value object responses such as status distribution', async () => {
    mocks.post.mockResolvedValue({ healthy: 2, unhealthy: 1, unknown: 0 })
    render(
      <BarChartWidget
        widget={makeWidget({ query: { source: 'souls', metric: 'status_distribution', time_range: '1h' } })}
        dashboardId="d1"
      />
    )
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
    expect(screen.queryByText('No data')).not.toBeInTheDocument()
  })
})
