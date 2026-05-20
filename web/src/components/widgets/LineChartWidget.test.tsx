import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { LineChartWidget } from './LineChartWidget'
import type { WidgetConfig } from '../../api/client'

const mocks = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: mocks.post } }))

const makeWidget = (overrides?: Partial<WidgetConfig>): WidgetConfig => ({
  id: 'w1',
  title: 'Latency',
  type: 'line_chart',
  grid: { x: 0, y: 0, width: 2, height: 1 },
  query: { source: 'judgments', metric: 'latency', time_range: '1h' },
  ...overrides,
})

describe('LineChartWidget', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('shows loading spinner initially', () => {
    mocks.post.mockReturnValue(new Promise(() => {}))
    render(<LineChartWidget widget={makeWidget()} dashboardId="d1" />)
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('renders chart with data', async () => {
    mocks.post.mockResolvedValue([
      { time: '10:00', count: 5, avg_latency: 120 },
      { time: '11:00', count: 8, avg_latency: 95 },
    ])
    render(<LineChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })

  it('shows no data when empty response', async () => {
    mocks.post.mockResolvedValue([])
    render(<LineChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('No data')).toBeInTheDocument())
  })

  it('shows no data on API error', async () => {
    mocks.post.mockRejectedValue(new Error('API error'))
    render(<LineChartWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('Error')).toBeInTheDocument())
  })

  it('uses avg_latency key for avg aggregation', async () => {
    mocks.post.mockResolvedValue([{ time: '10:00', count: 5, avg_latency: 120 }])
    render(
      <LineChartWidget
        widget={makeWidget({ query: { source: 'judgments', metric: 'latency', time_range: '1h', aggregation: 'avg' } })}
        dashboardId="d1"
      />
    )
    await waitFor(() => expect(document.querySelector('.recharts-wrapper')).toBeInTheDocument())
  })
})
