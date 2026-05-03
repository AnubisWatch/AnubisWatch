import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { Cluster } from './Cluster'

const clusterMocks = vi.hoisted(() => ({
  useClusterStatus: vi.fn(),
  useStats: vi.fn(),
}))

vi.mock('../api/hooks', () => ({
  useClusterStatus: clusterMocks.useClusterStatus,
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

  it('shows loading spinner', () => {
    mockCluster({ loading: true })
    mockStats({ loading: true })
    render(<Cluster />)
    expect(document.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('shows error state with try again button', () => {
    mockCluster({ loading: false, error: 'Cluster unreachable' })
    mockStats()
    render(<Cluster />)
    expect(screen.getByText('Cluster unreachable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })

  it('renders standalone mode', () => {
    mockCluster({
      data: { is_clustered: false, node_id: 'node-1', state: 'solo', term: 1, peer_count: 0 },
    })
    mockStats({ data: { souls: { total: 5 }, judgments: { today: 42 } } })
    render(<Cluster />)

    expect(screen.getByText('Standalone node configuration')).toBeInTheDocument()
    expect(screen.getByText('Standalone')).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByText('Disabled')).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: /node-1/ })).toBeInTheDocument()
    // solo appears in role card, state section, and node role cell.
    expect(screen.getAllByText('solo')).toHaveLength(3)
    // Term appears in overview and in Raft section (2 times)
    expect(screen.getAllByText('1')).toHaveLength(2)
  })

  it('renders clustered mode as leader', () => {
    mockCluster({
      data: { is_clustered: true, node_id: 'leader-1', state: 'leader', term: 7, peer_count: 3 },
    })
    mockStats({ data: { souls: { total: 12 }, judgments: { today: 999 } } })
    render(<Cluster />)

    expect(screen.getByText('Distributed monitoring nodes and Raft consensus')).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: /leader-1/ })).toBeInTheDocument()
    expect(screen.getAllByText('7')).toHaveLength(2)
    expect(screen.getAllByText('3')).toHaveLength(2)
    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText('999')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /join cluster/i })).toBeInTheDocument()
    // leader appears in role cell, state card, and table role badge
    expect(screen.getAllByText('leader')).toHaveLength(3)
  })

  it('renders clustered mode as follower', () => {
    mockCluster({
      data: { is_clustered: true, node_id: 'follower-2', state: 'follower', term: 7, peer_count: 3 },
    })
    mockStats()
    render(<Cluster />)

    expect(screen.getByRole('cell', { name: /follower-2/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /join cluster/i })).toBeInTheDocument()
    // follower appears in role cell, state card, and table role badge
    expect(screen.getAllByText('follower')).toHaveLength(3)
  })

  it('refreshes cluster and stats on button click', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const refetchCluster = vi.fn().mockResolvedValue(undefined)
    const refetchStats = vi.fn().mockResolvedValue(undefined)
    mockCluster({ data: { is_clustered: false, node_id: 'n1', state: 'solo', term: 1, peer_count: 0 }, refetch: refetchCluster })
    mockStats({ data: null, refetch: refetchStats })

    render(<Cluster />)
    const refreshBtn = screen.getByLabelText('Refresh cluster status')
    fireEvent.click(refreshBtn)

    await waitFor(() => {
      expect(refetchCluster).toHaveBeenCalled()
      expect(refetchStats).toHaveBeenCalled()
    })

    vi.advanceTimersByTime(600)
    await waitFor(() => expect(refreshBtn).not.toHaveClass('animate-spin'))
  })
})
