import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { StatusPages } from './StatusPages'

const mockCreatePage = vi.fn()
const mockUpdatePage = vi.fn()
const mockDeletePage = vi.fn()
const mockRefetch = vi.fn()
const mockClipboardWriteText = vi.fn()

let mockPages = [
  {
    id: 'page-1',
    name: 'Production Status',
    slug: 'production',
    description: 'Customer-facing services',
    enabled: true,
    theme: 'light' as const,
    souls: ['soul-1'],
    subscribers: 7,
  },
]

vi.mock('../api/hooks', () => ({
  useStatusPages: () => ({
    pages: mockPages,
    loading: false,
    error: null,
    refetch: mockRefetch,
    createPage: mockCreatePage,
    updatePage: mockUpdatePage,
    deletePage: mockDeletePage,
  }),
  useSouls: () => ({
    souls: [
      {
        id: 'soul-1',
        name: 'API',
        type: 'http',
        enabled: true,
      },
    ],
  }),
}))

describe('StatusPages', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPages = [
      {
        id: 'page-1',
        name: 'Production Status',
        slug: 'production',
        description: 'Customer-facing services',
        enabled: true,
        theme: 'light' as const,
        souls: ['soul-1'],
        subscribers: 7,
      },
    ]
    mockCreatePage.mockResolvedValue(undefined)
    mockUpdatePage.mockResolvedValue(undefined)
    mockDeletePage.mockResolvedValue(undefined)
    mockRefetch.mockResolvedValue(undefined)
    mockClipboardWriteText.mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockClipboardWriteText },
      configurable: true,
    })
    Object.defineProperty(navigator, 'share', {
      value: undefined,
      configurable: true,
    })
  })

  it('opens the edit modal with page values and saves through updatePage', async () => {
    render(<StatusPages />)

    fireEvent.click(screen.getByRole('button', { name: /edit status page production status/i }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Name')).toHaveValue('Production Status')
    expect(within(dialog).getByLabelText('Slug')).toHaveValue('production')
    expect(within(dialog).getByText('Light')).toHaveClass('border-amber-500')

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Updated Status' } })
    fireEvent.click(within(dialog).getByRole('button', { name: /save status page/i }))

    await waitFor(() => {
      expect(mockUpdatePage).toHaveBeenCalledWith('page-1', expect.objectContaining({
        name: 'Updated Status',
        slug: 'production',
        enabled: true,
        souls: ['soul-1'],
        subscribers: 7,
      }))
    })
    expect(mockCreatePage).not.toHaveBeenCalled()
  })

  it('falls back to copying the page URL when native share is unavailable', async () => {
    render(<StatusPages />)

    fireEvent.click(screen.getByRole('button', { name: /share status page production status/i }))

    await waitFor(() => {
      expect(mockClipboardWriteText).toHaveBeenCalledWith(expect.stringMatching(/\/status\/production$/))
    })
  })

  it('uses custom_domain for the domain count and external view link', () => {
    mockPages = [
      {
        id: 'page-custom-domain',
        name: 'Public Status',
        slug: 'public',
        description: 'Custom domain page',
        enabled: true,
        theme: 'dark' as const,
        custom_domain: 'status.example.com',
        souls: [],
        subscribers: 0,
      },
    ]

    render(<StatusPages />)

    expect(screen.getByText('Custom Domains').nextElementSibling).toHaveTextContent('1')
    expect(screen.getByRole('link', { name: /view/i })).toHaveAttribute('href', 'https://status.example.com')
  })
})
