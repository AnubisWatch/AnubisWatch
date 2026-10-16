import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Alerts } from './Alerts'

const mocks = vi.hoisted(() => ({
  createChannel: vi.fn(),
  updateChannel: vi.fn(),
  createRule: vi.fn(),
  updateRule: vi.fn(),
  channels: [] as unknown[],
  rules: [] as unknown[],
}))
vi.mock('../api/hooks', () => ({
  useChannels: () => ({ channels: mocks.channels, loading: false, createChannel: mocks.createChannel, updateChannel: mocks.updateChannel, refetch: vi.fn() }),
  useRules: () => ({ rules: mocks.rules, loading: false, createRule: mocks.createRule, updateRule: mocks.updateRule, refetch: vi.fn() }),
  useIncidents: () => ({ incidents: [], loading: false, refetch: vi.fn() }),
}))

describe('Alerts channels and rules', () => {
  beforeEach(() => {
    for (const fn of [mocks.createChannel, mocks.updateChannel, mocks.createRule, mocks.updateRule]) fn.mockReset().mockResolvedValue(undefined)
    mocks.channels = []
    mocks.rules = []
  })
  afterEach(cleanup)

  // Telegram, OpsGenie, ntfy and SMS dispatchers existed but the UI could not create them.
  it('creates a Telegram channel with its required fields', async () => {
    render(<Alerts />)
    fireEvent.click(screen.getByRole('tab', { name: /channels/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Add Channel' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('e.g., Ops Slack'), { target: { value: 'Ops Telegram' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Telegram' }))
    const save = within(dialog).getByRole('button', { name: 'Add Channel' })
    fireEvent.change(within(dialog).getByLabelText('Bot Token'), { target: { value: '123:abc' } })
    expect(save).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('Chat ID'), { target: { value: '-100' } })
    fireEvent.click(save)
    await waitFor(() => expect(mocks.createChannel).toHaveBeenCalledWith(expect.objectContaining({
      type: 'telegram',
      config: { bot_token: '123:abc', chat_id: '-100' },
    })))
  })

  // PUT replaces the channel; editing used to drop its delivery settings.
  it('keeps rate limit, retry policy and unknown config keys when editing', async () => {
    mocks.channels = [{
      id: 'c1', name: 'Hook', type: 'webhook', enabled: true,
      config: { url: 'https://hooks.example.com/a', secret: '[REDACTED]', headers: { 'X-A': '1' } },
      rate_limit: { max_per_hour: 10 }, retry_policy: { max_attempts: 5 }, filters: { severities: ['critical'] },
    }]
    render(<Alerts />)
    fireEvent.click(screen.getByRole('tab', { name: /channels/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Edit channel Hook' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('https://example.com/webhook'), { target: { value: 'https://hooks.example.com/b' } })
    fireEvent.click(within(dialog).getByRole('button', { name: /save/i }))
    await waitFor(() => expect(mocks.updateChannel).toHaveBeenCalled())
    const [, payload] = mocks.updateChannel.mock.calls[0]
    expect(payload.config).toEqual({ url: 'https://hooks.example.com/b', secret: '[REDACTED]', headers: { 'X-A': '1' } })
    expect(payload.rate_limit).toEqual({ max_per_hour: 10 })
    expect(payload.retry_policy).toEqual({ max_attempts: 5 })
    expect(payload.filters).toEqual({ severities: ['critical'] })
  })

  // Rules created in the UI used to have auto_resolve=false, so incidents never closed.
  it('creates rules with auto-resolve on and lets the user pick channels', async () => {
    mocks.channels = [
      { id: 'c1', name: 'Slack', type: 'slack', enabled: true, config: {} },
      { id: 'c2', name: 'Pager', type: 'pagerduty', enabled: true, config: {} },
    ]
    render(<Alerts />)
    fireEvent.click(screen.getByRole('tab', { name: /rules/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Add Rule' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByPlaceholderText('e.g., High Latency'), { target: { value: 'Down' } })
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /Pager/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add Rule' }))
    await waitFor(() => expect(mocks.createRule).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Down', auto_resolve: true, channels: ['c1'],
    })))
  })
})
