import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { NotFound } from './NotFound'

describe('NotFound', () => {
  it('renders 404 page with Egyptian theme', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    expect(screen.getByText('404')).toBeInTheDocument()
    expect(screen.getByText('Lost in the Duat')).toBeInTheDocument()
  })

  it('shows thematic message about Thoth', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    expect(screen.getByText(/Thoth cannot find/i)).toBeInTheDocument()
  })

  it('renders return home link', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    const homeLink = screen.getByRole('link', { name: /return home/i })
    expect(homeLink).toHaveAttribute('href', '/')
  })

  it('renders go back button', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    expect(screen.getByRole('button', { name: /go back/i })).toBeInTheDocument()
  })

  it('renders 404 decorative emoji', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    expect(screen.getByText('🏺')).toBeInTheDocument()
  })
})