import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { ConfirmDialog } from './ConfirmDialog'

describe('ConfirmDialog', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <ConfirmDialog
        open={false}
        title="Test"
        message="Are you sure?"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders the dialog when open', () => {
    render(
      <ConfirmDialog
        open={true}
        title="Delete Soul"
        message="This will permanently remove the soul."
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Delete Soul')).toBeInTheDocument()
    expect(screen.getByText('This will permanently remove the soul.')).toBeInTheDocument()
  })

  it('calls onCancel when clicking the backdrop', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        open={true}
        title="Test"
        message="Are you sure?"
        onConfirm={vi.fn()}
        onCancel={onCancel}
      />,
    )
    // The backdrop is the first child of the dialog container
    const backdrop = document.querySelector('.absolute.inset-0')
    if (backdrop) fireEvent.click(backdrop)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('calls onCancel when pressing Escape', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        open={true}
        title="Test"
        message="Are you sure?"
        onConfirm={vi.fn()}
        onCancel={onCancel}
      />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('calls onConfirm when clicking the confirm button', () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmDialog
        open={true}
        title="Delete"
        message="Permanently delete?"
        onConfirm={onConfirm}
        onCancel={vi.fn()}
        resourceName="test-soul"
      />,
    )
    const confirmButton = screen.getByLabelText('Confirm deletion of test-soul')
    fireEvent.click(confirmButton)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('calls onCancel when clicking the cancel button', () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        open={true}
        title="Test"
        message="Are you sure?"
        onConfirm={vi.fn()}
        onCancel={onCancel}
      />,
    )
    const cancelButton = screen.getByText('Cancel')
    fireEvent.click(cancelButton)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('renders with custom button labels', () => {
    render(
      <ConfirmDialog
        open={true}
        title="Test"
        message="Proceed?"
        confirmLabel="Yes, remove"
        cancelLabel="Keep it"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByText('Yes, remove')).toBeInTheDocument()
    expect(screen.getByText('Keep it')).toBeInTheDocument()
  })

  it('renders ReactNode message', () => {
    render(
      <ConfirmDialog
        open={true}
        title="Test"
        message={<span data-testid="custom-msg">Custom <strong>message</strong></span>}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByTestId('custom-msg')).toBeInTheDocument()
  })

  it('has accessible aria attributes', () => {
    render(
      <ConfirmDialog
        open={true}
        title="Delete Resource"
        message="This will be gone forever."
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAttribute('aria-labelledby')
    expect(dialog).toHaveAttribute('aria-describedby')
  })
})
