import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Alerts } from './Alerts'

const mockCreateChannel = vi.fn()
const mockUseChannels = vi.fn()
const mockUseRules = vi.fn()
const mockUseIncidents = vi.fn()

vi.mock('../api/hooks', () => ({
  useChannels: () => mockUseChannels(),
  useRules: () => mockUseRules(),
  useIncidents: () => mockUseIncidents(),
}))

describe('Alerts', () => {
  beforeEach(() => {
    mockCreateChannel.mockReset()
    mockUseChannels.mockReturnValue({
      channels: [],
      loading: false,
      error: null,
      refetch: vi.fn(),
      createChannel: mockCreateChannel,
      updateChannel: vi.fn(),
      deleteChannel: vi.fn(),
      testChannel: vi.fn(),
    })
    mockUseRules.mockReturnValue({
      rules: [],
      loading: false,
      error: null,
      refetch: vi.fn(),
      createRule: vi.fn(),
      updateRule: vi.fn(),
      deleteRule: vi.fn(),
    })
    mockUseIncidents.mockReturnValue({
      incidents: [],
      loading: false,
      error: null,
      refetch: vi.fn(),
      acknowledgeIncident: vi.fn(),
    })
  })

  it('creates a Discord channel with backend dispatcher config', async () => {
    mockCreateChannel.mockResolvedValue({ id: 'channel-1' })

    render(<Alerts />)

    fireEvent.click(screen.getByRole('tab', { name: /channels/i }))
    fireEvent.click(screen.getAllByRole('button', { name: /add channel/i })[0])
    fireEvent.change(screen.getByPlaceholderText('e.g., Ops Slack'), {
      target: { value: 'Ops Discord' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Discord' }))
    fireEvent.change(screen.getByPlaceholderText('https://discord.com/api/webhooks/...'), {
      target: { value: 'https://discord.com/api/webhooks/test' },
    })
    fireEvent.click(screen.getAllByRole('button', { name: 'Add Channel' }).at(-1)!)

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        name: 'Ops Discord',
        type: 'discord',
        enabled: true,
        config: { webhook_url: 'https://discord.com/api/webhooks/test' },
      })
    })
  })

  it('creates an email channel with SMTP and recipient config', async () => {
    mockCreateChannel.mockResolvedValue({ id: 'channel-1' })

    render(<Alerts />)

    fireEvent.click(screen.getByRole('tab', { name: /channels/i }))
    fireEvent.click(screen.getAllByRole('button', { name: /add channel/i })[0])
    fireEvent.change(screen.getByPlaceholderText('e.g., Ops Slack'), {
      target: { value: 'Ops Email' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Email' }))
    fireEvent.change(screen.getByPlaceholderText('smtp.example.com'), {
      target: { value: 'smtp.example.com' },
    })
    fireEvent.change(screen.getByPlaceholderText('alerts@example.com'), {
      target: { value: 'alerts@example.com' },
    })
    fireEvent.change(screen.getByPlaceholderText('ops@example.com, oncall@example.com'), {
      target: { value: 'ops@example.com, oncall@example.com' },
    })
    fireEvent.click(screen.getAllByRole('button', { name: 'Add Channel' }).at(-1)!)

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        name: 'Ops Email',
        type: 'email',
        enabled: true,
        config: {
          smtp_host: 'smtp.example.com',
          smtp_port: 587,
          from: 'alerts@example.com',
          to: ['ops@example.com', 'oncall@example.com'],
        },
      })
    })
  })
})
