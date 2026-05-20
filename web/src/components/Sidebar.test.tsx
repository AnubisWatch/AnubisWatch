import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
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
})
