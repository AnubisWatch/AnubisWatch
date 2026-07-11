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

  it('ignores settled requests after unmount', async () => {
    let resolve!: (value: Array<Record<string, unknown>>) => void
    mocks.post.mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const view = render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    view.unmount(); resolve([]); await Promise.resolve()
    let reject!: (reason: unknown) => void
    mocks.post.mockReturnValueOnce(new Promise((_, fail) => { reject = fail }))
    const second = render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    second.unmount(); reject('bad'); await Promise.resolve()
  })

  it('uses non-Error fallback and null response handling', async () => {
    mocks.post.mockRejectedValueOnce('bad')
    const view = render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    expect(await screen.findByText('Failed to load')).toBeInTheDocument(); view.unmount()
    mocks.post.mockResolvedValueOnce(null)
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    expect(await screen.findByText('No data')).toBeInTheDocument()
  })

  it('handles an entry that disappears before column extraction', async () => {
    const changing: Array<Record<string, unknown>> = []
    changing.length = 1
    Object.defineProperty(changing, 0, { get: () => undefined })
    mocks.post.mockResolvedValue(changing)
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    await screen.findByRole('table')
    expect(screen.queryAllByRole('columnheader')).toHaveLength(0)
  })

  it('limits columns and rows and renders null cells', async () => {
    mocks.post.mockResolvedValue(Array.from({ length: 21 }, (_, i) => ({ one: i, two: null, three: 3, four: 4, five: 5, six: 6 })))
    render(<TableWidget widget={makeWidget()} dashboardId="d1" />)
    await screen.findByRole('table')
    expect(screen.getAllByRole('columnheader')).toHaveLength(5)
    expect(screen.getAllByRole('row')).toHaveLength(21)
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
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
