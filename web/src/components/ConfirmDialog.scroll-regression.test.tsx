import { render } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ConfirmDialog } from './ConfirmDialog'
afterEach(() => { document.body.style.overflow = '' })
const props = { title: 'Confirm', message: 'Proceed?', onCancel: vi.fn(), onConfirm: vi.fn() }
it.each(['', 'scroll', 'hidden', 'auto'])('restores prior overflow %j after open-close-unmount cycles', overflow => {
  document.body.style.overflow = overflow
  const view = render(<ConfirmDialog {...props} open />)
  expect(document.body.style.overflow).toBe('hidden')
  view.rerender(<ConfirmDialog {...props} open={false} />)
  expect(document.body.style.overflow).toBe(overflow)
  view.rerender(<ConfirmDialog {...props} open />)
  expect(document.body.style.overflow).toBe('hidden')
  view.unmount()
  expect(document.body.style.overflow).toBe(overflow)
})
