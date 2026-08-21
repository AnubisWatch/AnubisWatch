import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from '../components/Sidebar'

// Mock useAuth hook
vi.mock('../api/hooks', () => ({
  useAuth: () => ({
    logout: vi.fn(),
  }),
}))

describe('Sidebar', () => {
  it('renders Anubis logo and branding', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )

    expect(screen.getByText('Anubis')).toBeInTheDocument()
    expect(screen.getByText('Watch')).toBeInTheDocument()
    expect(screen.getByText('"The Judgment Never Sleeps"')).toBeInTheDocument()
  })

  it('renders all navigation items', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )

    const navItems = ['Hall of Judgment', 'Essence', 'Weighings', 'Divine Warnings', 'Cries of Chaos', 'Sacred Rest', 'Voyages', 'Sacred Charts', 'Necropolis', 'Temple Squares', 'Pharaoh\'s Chamber']
    navItems.forEach(item => {
      expect(screen.getByText(item)).toBeInTheDocument()
    })
  })

  it('renders status indicator', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )

    expect(screen.getByText("Ma'at Balanced")).toBeInTheDocument()
    expect(screen.getByText('99.9% uptime')).toBeInTheDocument()
  })

  it('renders logout button', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )

    expect(screen.getByText('Leave the Temple')).toBeInTheDocument()
  })

  it('renders Hall of Ma\'at section header', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )

    expect(screen.getByText("Hall of Ma'at")).toBeInTheDocument()
  })

  describe('mobile drawer', () => {
    it('is closed (off-canvas) by default', () => {
      const { container } = render(
        <MemoryRouter>
          <Sidebar />
        </MemoryRouter>
      )

      const aside = container.querySelector('aside')
      expect(aside?.className).toContain('-translate-x-full')
    })

    it('slides in and shows a backdrop when open', () => {
      const { container } = render(
        <MemoryRouter>
          <Sidebar open onClose={vi.fn()} />
        </MemoryRouter>
      )

      const aside = container.querySelector('aside')
      expect(aside?.className).toContain('translate-x-0')
      expect(container.querySelector('[aria-hidden="true"].fixed.inset-0')).toBeInTheDocument()
    })

    it('calls onClose when the backdrop is clicked', () => {
      const onClose = vi.fn()
      const { container } = render(
        <MemoryRouter>
          <Sidebar open onClose={onClose} />
        </MemoryRouter>
      )

      const backdrop = container.querySelector('[aria-hidden="true"].fixed.inset-0') as HTMLElement
      fireEvent.click(backdrop)
      expect(onClose).toHaveBeenCalledTimes(1)
    })

    it('calls onClose when the close button is clicked', () => {
      const onClose = vi.fn()
      render(
        <MemoryRouter>
          <Sidebar open onClose={onClose} />
        </MemoryRouter>
      )

      fireEvent.click(screen.getByLabelText('Close navigation menu'))
      expect(onClose).toHaveBeenCalledTimes(1)
    })

    it('calls onClose when Escape is pressed while open', () => {
      const onClose = vi.fn()
      render(
        <MemoryRouter>
          <Sidebar open onClose={onClose} />
        </MemoryRouter>
      )

      fireEvent.keyDown(document, { key: 'Escape' })
      expect(onClose).toHaveBeenCalledTimes(1)
    })

    it('ignores other keys while open', () => {
      const onClose = vi.fn()
      render(
        <MemoryRouter>
          <Sidebar open onClose={onClose} />
        </MemoryRouter>
      )

      fireEvent.keyDown(document, { key: 'Enter' })
      expect(onClose).not.toHaveBeenCalled()
    })

    it('does not listen for Escape while closed', () => {
      const onClose = vi.fn()
      render(
        <MemoryRouter>
          <Sidebar open={false} onClose={onClose} />
        </MemoryRouter>
      )

      fireEvent.keyDown(document, { key: 'Escape' })
      expect(onClose).not.toHaveBeenCalled()
    })

    it('calls onClose when a navigation link is clicked', () => {
      const onClose = vi.fn()
      render(
        <MemoryRouter>
          <Sidebar open onClose={onClose} />
        </MemoryRouter>
      )

      fireEvent.click(screen.getByText('Essence'))
      expect(onClose).toHaveBeenCalledTimes(1)
    })
  })
})
