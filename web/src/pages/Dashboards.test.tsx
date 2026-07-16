import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Dashboards } from './Dashboards'

const mockDeleteDashboard = vi.fn()
const mockUseDashboards = vi.fn()

vi.mock('../api/hooks', () => ({
  useDashboards: () => mockUseDashboards(),
}))

describe('Dashboards', () => {
  beforeEach(() => {
    mockDeleteDashboard.mockClear()
    mockUseDashboards.mockClear()
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  it('renders loading state', () => {
    mockUseDashboards.mockReturnValue({ dashboards: [], loading: true, deleteDashboard: mockDeleteDashboard })

    render(
      <MemoryRouter>
        <Dashboards />
      </MemoryRouter>
    )

    expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('renders empty state', () => {
    mockUseDashboards.mockReturnValue({ dashboards: [], loading: false, deleteDashboard: mockDeleteDashboard })

    render(
      <MemoryRouter>
        <Dashboards />
      </MemoryRouter>
    )

    expect(screen.getByText('No Dashboards Yet')).toBeInTheDocument()
    expect(screen.getByText('Create Dashboard')).toBeInTheDocument()
  })

  it('renders dashboard list with widgets', () => {
    const dashboards = [
      {
        id: 'dash-1',
        name: 'Test Dashboard',
        description: 'A test dashboard',
        widgets: [
          { id: 'w1', type: 'line_chart', title: 'Latency' },
          { id: 'w2', type: 'bar_chart', title: 'Errors' },
          { id: 'w3', type: 'gauge', title: 'CPU' },
          { id: 'w4', type: 'stat', title: 'Uptime' },
          { id: 'w5', type: 'table', title: 'Logs' },
        ],
        refresh_sec: 30,
      },
    ]
    mockUseDashboards.mockReturnValue({ dashboards, loading: false, deleteDashboard: mockDeleteDashboard })

    render(
      <MemoryRouter>
        <Dashboards />
      </MemoryRouter>
    )

    expect(screen.getByText('Test Dashboard')).toBeInTheDocument()
    expect(screen.getByText('A test dashboard')).toBeInTheDocument()
    expect(screen.getByText('+1 more')).toBeInTheDocument()
    expect(screen.getByText('Refreshes every 30s')).toBeInTheDocument()
    expect(screen.getByLabelText('Delete dashboard Test Dashboard')).toBeInTheDocument()
  }
  )

  it('deletes a dashboard after confirm', async () => {
    const dashboards = [{ id: 'dash-1', name: 'Test Dashboard', widgets: [], refresh_sec: 0 }]
    mockUseDashboards.mockReturnValue({ dashboards, loading: false, deleteDashboard: mockDeleteDashboard })
    mockDeleteDashboard.mockResolvedValue(undefined)

    render(
      <MemoryRouter>
        <Dashboards />
      </MemoryRouter>
    )

    fireEvent.click(screen.getByLabelText('Delete dashboard Test Dashboard'))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))

    await waitFor(() => {
      expect(mockDeleteDashboard).toHaveBeenCalledWith('dash-1')
    })
  })

  it('cancels delete when user declines confirm', async () => {
    const dashboards = [{ id: 'dash-1', name: 'Test Dashboard', widgets: [], refresh_sec: 0 }]
    mockUseDashboards.mockReturnValue({ dashboards, loading: false, deleteDashboard: mockDeleteDashboard })

    render(
      <MemoryRouter>
        <Dashboards />
      </MemoryRouter>
    )

    fireEvent.click(screen.getByLabelText('Delete dashboard Test Dashboard'))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByText('Cancel'))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(mockDeleteDashboard).not.toHaveBeenCalled()
  })
})
