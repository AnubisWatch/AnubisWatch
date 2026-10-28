import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WorkspaceQuotaPanel } from './WorkspaceQuotaPanel'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api/client', () => ({ api }))
const mockGet = api.get

const usage = {
  workspace: 'default',
  enforced: true,
  souls: { used: 5, limit: 5 },
  journeys: { used: 1, limit: 0 },
  alert_channels: { used: 4, limit: 5 },
  check_interval_min: '30s',
  retention_days: 90,
}

describe('WorkspaceQuotaPanel', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it('shows usage against each limit', async () => {
    mockGet.mockResolvedValue(usage)
    render(<WorkspaceQuotaPanel />)

    expect(await screen.findByText('5 / 5')).toHaveClass('text-rose-400')
    expect(mockGet).toHaveBeenCalledWith('/quota')
    expect(screen.getByText('1 (unlimited)')).toBeInTheDocument()
    expect(screen.getByText('4 / 5')).toBeInTheDocument()
    expect(screen.getByRole('progressbar', { name: 'Souls quota' })).toHaveAttribute('aria-valuenow', '5')
    expect(screen.queryByRole('progressbar', { name: 'Journeys quota' })).not.toBeInTheDocument()
    expect(screen.getByText('30s')).toBeInTheDocument()
    expect(screen.getByText('90 days')).toBeInTheDocument()
  })

  it('says nothing is limited when quotas are not enforced', async () => {
    mockGet.mockResolvedValue({
      ...usage,
      enforced: false,
      souls: { used: 12, limit: 0 },
      alert_channels: { used: 0, limit: 0 },
      check_interval_min: '0s',
      retention_days: 0,
    })
    render(<WorkspaceQuotaPanel />)

    expect(await screen.findByText(/not enforced on this server/)).toBeInTheDocument()
    expect(screen.getByText('12 (unlimited)')).toBeInTheDocument()
    expect(screen.queryByText('Minimum check interval')).not.toBeInTheDocument()
  })

  it('shows a load failure', async () => {
    mockGet.mockRejectedValue(new Error('forbidden'))
    await act(async () => {
      render(<WorkspaceQuotaPanel />)
    })
    expect(screen.getByRole('alert')).toHaveTextContent('forbidden')
  })
})
