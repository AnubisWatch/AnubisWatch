import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
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

  it('navigates back when go back is clicked', () => {
    const back = vi.spyOn(window.history, 'back').mockImplementation(() => undefined)
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /go back/i }))
    expect(back).toHaveBeenCalledOnce()
    back.mockRestore()
  })

  it('renders 404 decorative emoji', () => {
    render(<MemoryRouter><NotFound /></MemoryRouter>)
    expect(screen.getByText('🏺')).toBeInTheDocument()
  })
})