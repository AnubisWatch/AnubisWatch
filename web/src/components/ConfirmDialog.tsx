import { useEffect, useRef, type ReactNode } from 'react'
import { AlertTriangle, X } from 'lucide-react'

interface ConfirmDialogProps {
  /** Whether the dialog is open */
  open: boolean
  /** Dialog title */
  title: string
  /** Dialog body message */
  message: ReactNode
  /** Label for the confirm (destructive) button */
  confirmLabel?: string
  /** Label for the cancel button */
  cancelLabel?: string
  /** Called when the user confirms */
  onConfirm: () => void
  /** Called when the user cancels or closes */
  onCancel: () => void
  /** The resource name being deleted (for aria-label) */
  resourceName?: string
}

/**
 * ConfirmDialog is an accessible modal dialog for confirming destructive
 * actions. Uses a portal rendered at the document body level.
 *
 * Accessibility:
 * - Focus is trapped inside the dialog while open
 * - Escape closes the dialog
 * - Clicking the backdrop closes the dialog
 * - ARIA roles and labels are set correctly
 * - Initial focus goes to the cancel button (safe default)
 */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = 'Delete',
  cancelLabel = 'Cancel',
  onConfirm,
  onCancel,
  resourceName,
}: ConfirmDialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const previousFocusRef = useRef<HTMLElement | null>(null)

  // Trap focus and handle Escape
  useEffect(() => {
    if (!open) return

    previousFocusRef.current = document.activeElement as HTMLElement | null

    // Focus the cancel button on open
    requestAnimationFrame(() => {
      cancelRef.current?.focus()
    })

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onCancel()
        return
      }

      // Trap focus within the dialog
      if (e.key === 'Tab' && dialogRef.current) {
        const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        )
        if (focusable.length === 0) return

        const first = focusable[0]
        const last = focusable[focusable.length - 1]

        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault()
            first.focus()
          }
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    // Prevent body scroll while dialog is open
    document.body.style.overflow = 'hidden'

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      document.body.style.overflow = ''
      // Restore focus to the previously focused element
      previousFocusRef.current?.focus()
    }
  }, [open, onCancel])

  if (!open) return null

  const titleId = 'confirm-dialog-title'
  const descriptionId = 'confirm-dialog-description'

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onCancel}
        aria-hidden="true"
      />

      {/* Dialog panel */}
      <div
        ref={dialogRef}
        className="relative w-full max-w-md rounded-xl border border-[var(--border-strong)] bg-[var(--bg-card)] p-6 shadow-2xl"
      >
        {/* Close button */}
        <button
          onClick={onCancel}
          className="absolute right-4 top-4 rounded-lg p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--accent-primary-soft)] hover:text-[var(--accent-primary)]"
          aria-label="Close dialog"
        >
          <X className="h-4 w-4" />
        </button>

        {/* Icon */}
        <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full" style={{ backgroundColor: 'color-mix(in srgb, var(--danger) 15%, transparent)' }}>
          <AlertTriangle className="h-6 w-6" style={{ color: 'var(--danger)' }} />
        </div>

        {/* Title */}
        <h2
          id={titleId}
          className="mb-2 text-lg font-semibold text-[var(--text-primary)]"
        >
          {title}
        </h2>

        {/* Message */}
        <p
          id={descriptionId}
          className="mb-6 text-sm leading-relaxed text-[var(--text-secondary)]"
        >
          {message}
        </p>

        {/* Actions */}
        <div className="flex justify-end gap-3">
          <button
            ref={cancelRef}
            onClick={onCancel}
            className="rounded-lg border border-[var(--border-color)] bg-transparent px-4 py-2 text-sm font-medium text-[var(--text-primary)] transition-colors hover:bg-[var(--accent-primary-soft)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-primary)]"
          >
            {cancelLabel}
          </button>
          <button
            onClick={onConfirm}
            className="rounded-lg px-4 py-2 text-sm font-medium text-white transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--danger)]"
            style={{ backgroundColor: 'var(--danger)' }}
            onMouseOver={(e) => (e.currentTarget.style.filter = 'brightness(0.8)')}
            onMouseOut={(e) => (e.currentTarget.style.filter = '')}
            aria-label={resourceName ? `Confirm deletion of ${resourceName}` : 'Confirm'}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
