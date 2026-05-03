import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { EventsFeed } from './EventsFeed'
import { useWebSocket, type WebSocketMessage } from '../hooks/webSocketContext'

// Mock date utils to avoid time-based flakiness
vi.mock('../utils/date', () => ({
  formatDistanceToNow: () => '2m ago'
}))

vi.mock('../hooks/webSocketContext', () => ({
  useWebSocket: vi.fn()
}))

const mockMessages: WebSocketMessage[] = [
  {
    type: 'judgment',
    data: { id: 'j-1', status: 'alive', soul_name: 'API Server' },
    timestamp: '2026-05-04T00:00:00Z'
  },
  {
    type: 'alert',
    data: { id: 'a-1', severity: 'warning', message: 'High latency detected', soul_name: 'Database' },
    timestamp: '2026-05-04T00:01:00Z'
  },
  {
    type: 'incident',
    data: { id: 'i-1', severity: 'critical', status: 'open', soul_name: 'Checkout API' },
    timestamp: '2026-05-04T00:02:00Z'
  }
]

describe('EventsFeed', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(useWebSocket).mockReturnValue({
      connected: true,
      messages: mockMessages,
      send: vi.fn(),
      lastMessage: mockMessages[2],
      connect: vi.fn(),
      disconnect: vi.fn()
    })
  })

  it('renders live WebSocket events', () => {
    render(<EventsFeed />)

    expect(screen.getByText('Health check passed')).toBeInTheDocument()
    expect(screen.getByText('High latency detected')).toBeInTheDocument()
    expect(screen.getByText('Incident open')).toBeInTheDocument()
  })

  it('limits events by maxEvents prop', () => {
    render(<EventsFeed maxEvents={2} />)

    expect(screen.getByText('Health check passed')).toBeInTheDocument()
    expect(screen.getByText('High latency detected')).toBeInTheDocument()
    expect(screen.queryByText('Incident open')).not.toBeInTheDocument()
  })

  it('dismisses an event when clicking the dismiss button', async () => {
    render(<EventsFeed />)

    expect(screen.getByText('Health check passed')).toBeInTheDocument()

    const dismissButtons = screen.getAllByLabelText('Dismiss event')
    expect(dismissButtons).toHaveLength(3)

    fireEvent.click(dismissButtons[0])

    await waitFor(() => {
      expect(screen.queryByText('Health check passed')).not.toBeInTheDocument()
    })

    expect(screen.getByText('High latency detected')).toBeInTheDocument()
    expect(screen.getByText('Incident open')).toBeInTheDocument()
  })

  it('shows empty state when all events are dismissed', async () => {
    render(<EventsFeed />)

    const dismissButtons = screen.getAllByLabelText('Dismiss event')

    // Dismiss all events
    for (const button of dismissButtons) {
      fireEvent.click(button)
    }

    await waitFor(() => {
      expect(screen.getByText('No recent events')).toBeInTheDocument()
    })
  })

  it('renders soul name when present', () => {
    render(<EventsFeed />)

    expect(screen.getByText(/API Server/)).toBeInTheDocument()
    expect(screen.getByText(/Database/)).toBeInTheDocument()
    expect(screen.getByText(/Checkout API/)).toBeInTheDocument()
  })

  it('displays formatted timestamps', () => {
    render(<EventsFeed />)

    const timestamps = screen.getAllByText('2m ago')
    expect(timestamps).toHaveLength(3)
  })

  it('shows empty state when no live events have arrived', () => {
    vi.mocked(useWebSocket).mockReturnValue({
      connected: true,
      messages: [],
      send: vi.fn(),
      lastMessage: null,
      connect: vi.fn(),
      disconnect: vi.fn()
    })

    render(<EventsFeed />)

    expect(screen.getByText('No recent events')).toBeInTheDocument()
  })
})
