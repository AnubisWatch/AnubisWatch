import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { StatWidget } from './StatWidget'
import type { WidgetConfig } from '../../api/client'

const mocks = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: mocks.post } }))

const makeWidget = (overrides?: Partial<WidgetConfig>): WidgetConfig => ({
  id: 'w1',
  title: 'Test Stat',
  type: 'stat',
  grid: { x: 0, y: 0, width: 1, height: 1 },
  query: { source: 'prometheus', metric: 'cpu_usage', time_range: '1h' },
  ...overrides,
})

describe('StatWidget', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows loading spinner initially', () => {
    mocks.post.mockReturnValue(new Promise(() => {}))
    render(<StatWidget widget={makeWidget()} dashboardId="d1" />)
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('displays numeric value with locale formatting', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 1234567 })
    render(<StatWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText(/1[,.]234[,.]567/)).toBeInTheDocument())
  })

  it('uses the configured metric key when multiple values are returned', async () => {
    mocks.post.mockResolvedValue({ total: 10, uptime: 99.5 })
    render(<StatWidget widget={makeWidget({ query: { source: 'stats', metric: 'uptime', time_range: '1h' } })} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('99.5')).toBeInTheDocument())
  })

  it('displays the metric label', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 42 })
    render(<StatWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('cpu_usage')).toBeInTheDocument())
  })

  it('handles API error gracefully', async () => {
    mocks.post.mockRejectedValue(new Error('API error'))
    render(<StatWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('—')).toBeInTheDocument())
  })

  it('shows dash for empty response', async () => {
    mocks.post.mockResolvedValue({})
    render(<StatWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('—')).toBeInTheDocument())
  })
})
