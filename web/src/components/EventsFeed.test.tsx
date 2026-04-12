import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { EventsFeed } from './EventsFeed'

// Mock date utils to avoid time-based flakiness
vi.mock('../utils/date', () => ({
  formatDistanceToNow: () => '2m ago'
}))

describe('EventsFeed', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders initial events', () => {
    render(<EventsFeed />)

    expect(screen.getByText('Health check passed')).toBeInTheDocument()
    expect(screen.getByText('Configuration updated')).toBeInTheDocument()
    expect(screen.getByText('High latency detected')).toBeInTheDocument()
  })

  it('limits events by maxEvents prop', () => {
    render(<EventsFeed maxEvents={2} />)

    expect(screen.getByText('Health check passed')).toBeInTheDocument()
    expect(screen.getByText('Configuration updated')).toBeInTheDocument()
    expect(screen.queryByText('High latency detected')).not.toBeInTheDocument()
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

    expect(screen.getByText('Configuration updated')).toBeInTheDocument()
    expect(screen.getByText('High latency detected')).toBeInTheDocument()
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
  })

  it('displays formatted timestamps', () => {
    render(<EventsFeed />)

    const timestamps = screen.getAllByText('2m ago')
    expect(timestamps).toHaveLength(3)
  })
})
