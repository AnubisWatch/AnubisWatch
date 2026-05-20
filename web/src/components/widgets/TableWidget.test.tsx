import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { TableWidget } from './TableWidget'
import type { WidgetConfig } from '../../api/client'

const mocks = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../../api/client', () => ({ api: { post: mocks.post } }))

const makeWidget = (overrides?: Partial<WidgetConfig>): WidgetConfig => ({
  id: 'w1',
  title: 'Test Table',
  type: 'table',
  grid: { x: 0, y: 0, width: 2, height: 1 },
  query: { source: 'prometheus', metric: 'services', time_range: '1h' },
  ...overrides,
})

describe('TableWidget', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('shows loading spinner initially', () => {
    mocks.post.mockReturnValue(new Promise(() => {}))
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    const spinner = document.querySelector('.animate-spin')
    expect(spinner).toBeInTheDocument()
  })

  it('renders table with data rows', async () => {
    mocks.post.mockResolvedValue([
      { name: 'Service A', status: true, latency: 45 },
      { name: 'Service B', status: false, latency: 0 },
    ])
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('Service A')).toBeInTheDocument())
    expect(screen.getByText('Service B')).toBeInTheDocument()
  })

  it('handles empty data gracefully', async () => {
    mocks.post.mockResolvedValue([])
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('No data')).toBeInTheDocument())
  })

  it('handles API error gracefully', async () => {
    mocks.post.mockRejectedValue(new Error('API error'))
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    await waitFor(() => expect(screen.getByText('Error')).toBeInTheDocument())
  })
})
