import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { GaugeWidget } from './GaugeWidget'
import type { WidgetConfig } from '../../api/client'

const mocks = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: mocks.post } }))

const makeWidget = (overrides?: Partial<WidgetConfig>): WidgetConfig => ({
  id: 'w1',
  title: 'CPU Gauge',
  type: 'gauge',
  grid: { x: 0, y: 0, width: 1, height: 1 },
  query: { source: 'prometheus', metric: 'cpu_usage', time_range: '1h' },
  ...overrides,
})

describe('GaugeWidget', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('shows loading spinner initially', () => {
    mocks.post.mockReturnValue(new Promise(() => {}))
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('ignores a response that settles after unmount', async () => {
    let resolve!: (value: Record<string, number>) => void
    mocks.post.mockReturnValue(new Promise((done) => { resolve = done }))
    const view = render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    view.unmount()
    resolve({ cpu_usage: 1 })
    await Promise.resolve()
  })

  it('uses the first value, treats non-numbers as zero, and applies default bands', async () => {
    mocks.post.mockResolvedValue({ other: 25 })
    const view = render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await screen.findByText('25.0%')
    view.unmount()
    mocks.post.mockResolvedValue({ cpu_usage: 'bad' })
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await screen.findByText('0.0%')
  })

  it('uses the amber default band', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 60 })
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await screen.findByText('60.0%')
  })

  it('continues past non-matching thresholds', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 60 })
    render(<GaugeWidget widget={makeWidget({ thresholds: [{ value: 10, color: 'x', op: 'lt' }, { value: 50, color: 'y', op: 'gt' }] })} dashboardId="d1" />)
    await screen.findByText('60.0%')
  })

  it('displays value as percentage', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 75.5 })
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('75.5%')).toBeInTheDocument())
  })

  it('displays widget title', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 50 })
    render(<GaugeWidget widget={makeWidget({ title: 'CPU Usage' })} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('CPU Usage')).toBeInTheDocument())
  })

  it('handles empty and null responses gracefully', async () => {
    mocks.post.mockResolvedValue({})
    const view = render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('0.0%')).toBeInTheDocument())
    view.unmount()
    mocks.post.mockResolvedValue(null)
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('0.0%')).toBeInTheDocument())
  })

  it('handles API error gracefully', async () => {
    mocks.post.mockRejectedValue(new Error('API error'))
    render(<GaugeWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('0.0%')).toBeInTheDocument())
  })

  it('normalizes uptime metric to max 100', async () => {
    mocks.post.mockResolvedValue({ uptime: 150 })
    render(<GaugeWidget widget={makeWidget({ query: { source: 'prometheus', metric: 'uptime', time_range: '1h' } })} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('100.0%')).toBeInTheDocument())
  })

  it('uses the configured metric key when multiple values are returned', async () => {
    mocks.post.mockResolvedValue({ total: 10, uptime: 75 })
    render(<GaugeWidget widget={makeWidget({ query: { source: 'stats', metric: 'uptime', time_range: '1h' } })} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('75.0%')).toBeInTheDocument())
  })

  it('applies lt threshold color when value is below threshold', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 30 })
    render(
      <GaugeWidget
        widget={makeWidget({
          thresholds: [
            { value: 50, color: '#f43f5e', op: 'lt' },
            { value: 80, color: '#f59e0b', op: 'lt' },
          ]
        })}
        dashboardId="d1"
      />
    )
    await waitFor(() => expect(screen.getByText('30.0%')).toBeInTheDocument())
  })

  it('applies gt threshold color when value is above threshold', async () => {
    mocks.post.mockResolvedValue({ cpu_usage: 90 })
    render(
      <GaugeWidget
        widget={makeWidget({
          thresholds: [
            { value: 80, color: '#f59e0b', op: 'gt' },
          ]
        })}
        dashboardId="d1"
      />
    )
    await waitFor(() => expect(screen.getByText('90.0%')).toBeInTheDocument())
  })
})
