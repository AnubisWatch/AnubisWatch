import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { Cluster } from './Cluster'

const clusterMocks = vi.hoisted(() => ({
  useClusterStatus: vi.fn(),
  useClusterPeers: vi.fn(),
  useStats: vi.fn(),
}))

vi.mock('../api/hooks', () => ({
  useClusterStatus: clusterMocks.useClusterStatus,
  useClusterPeers: clusterMocks.useClusterPeers,
  useStats: clusterMocks.useStats,
}))

describe('Cluster', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  const mockCluster = (overrides?: Partial<ReturnType<typeof clusterMocks.useClusterStatus>>) => {
    clusterMocks.useClusterStatus.mockReturnValue({
      data: null,
      loading: false,
      error: null,
      refetch: vi.fn(),
      ...overrides,
    })
  }

  const mockStats = (overrides?: Partial<ReturnType<typeof clusterMocks.useStats>>) => {
    clusterMocks.useStats.mockReturnValue({
      data: null,
      loading: false,
      error: null,
      refetch: vi.fn(),
      ...overrides,
    })
  }

  const mockPeers = (overrides?: Partial<ReturnType<typeof clusterMocks.useClusterPeers>>) => {
    clusterMocks.useClusterPeers.mockReturnValue({
      data: null,
      loading: false,
      refetch: vi.fn(),
      ...overrides,
    })
  }

  it('shows loading spinner', () => {
    mockCluster({ loading: true })
    mockPeers({ loading: true })
    mockStats({ loading: true })
    render(<Cluster />)
    expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows error state with try again button', () => {
    mockCluster({ loading: false, error: 'Cluster unreachable' })
    mockPeers()
    mockStats()
    render(<Cluster />)
    expect(screen.getByText('Cluster unreachable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })

  it('renders standalone mode', () => {
    mockCluster({
      data: { is_clustered: false, node_id: 'node-1', state: 'solo', term: 1, peer_count: 0 },
    })
    mockPeers({ data: [] })
    mockStats({ data: { souls: { total: 5 }, judgments: { today: 42 } } })
    render(<Cluster />)

    // "solo" appears in role label, role cell (2x), and node role cell (4 total)
    expect(screen.getAllByText('solo')).toHaveLength(4)
  })

  it('renders clustered mode as leader', () => {
    mockCluster({
      data: { is_clustered: true, node_id: 'leader-1', state: 'leader', term: 7, peer_count: 3 },
    })
    mockPeers({ data: [{ id: 'leader-1', name: 'leader-1', address: 'localhost', state: 'leader', last_contact: 'now' }] })
    mockStats({ data: { souls: { total: 12 }, judgments: { today: 999 } } })
    render(<Cluster />)

    expect(screen.getByRole('cell', { name: /leader-1/ })).toBeInTheDocument()
    expect(screen.getAllByText('7')).toHaveLength(2)
    expect(screen.getAllByText('3')).toHaveLength(2)
    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /join cluster/i })).not.toBeInTheDocument()
    expect(screen.getByText('Add Node')).toBeInTheDocument()
    // leader appears in status cell (label + badge), role cell (label + badge) = 4 times
    expect(screen.getAllByText('leader')).toHaveLength(4)
  })

  it('renders clustered mode as follower', () => {
    mockCluster({
      data: { is_clustered: true, node_id: 'follower-2', state: 'follower', term: 7, peer_count: 3 },
    })
    mockPeers({ data: [{ id: 'follower-2', name: 'follower-2', address: 'localhost', state: 'follower', last_contact: 'now' }] })
    mockStats()
    render(<Cluster />)

    expect(screen.getByRole('cell', { name: /follower-2/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /join cluster/i })).not.toBeInTheDocument()
    expect(screen.getByText('Add Node')).toBeInTheDocument()
    // follower appears in status cell (label + badge), role cell (label + badge) = 4 times
    expect(screen.getAllByText('follower')).toHaveLength(4)
  })

  it('refreshes cluster and stats on button click', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const refetchCluster = vi.fn().mockResolvedValue(undefined)
    const refetchPeers = vi.fn().mockResolvedValue(undefined)
    mockCluster({ data: { is_clustered: false, node_id: 'n1', state: 'solo', term: 1, peer_count: 0 }, refetch: refetchCluster })
    mockPeers({ data: [], refetch: refetchPeers })

    render(<Cluster />)
    const refreshBtn = screen.getByLabelText('Refresh cluster status')
    fireEvent.click(refreshBtn)

    await waitFor(() => {
      expect(refetchCluster).toHaveBeenCalled()
      expect(refetchPeers).toHaveBeenCalled()
    })

    vi.advanceTimersByTime(600)
    await waitFor(() => expect(refreshBtn).not.toHaveClass('animate-spin'))
  })
})
