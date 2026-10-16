import { describe, expect, it } from 'vitest'
import type { Soul } from '../api/client'
import { buildSoulPayload, defaultSoulFormData, soulFormDataFromSoul, soulTypeOptions } from './soulForm'

// IMAP checks existed in the backend but could not be created from the UI.
describe('IMAP souls', () => {
  it('is offered as a type', () => {
    expect(soulTypeOptions.map(o => o.value)).toContain('imap')
  })

  it('round-trips the IMAP block', () => {
    const payload = buildSoulPayload({
      ...defaultSoulFormData, name: 'Mail', type: 'imap', target: 'mail.example.com:993',
      imapTLS: true, imapMailbox: 'INBOX', imapUsername: 'probe', imapPassword: 'pw',
    })
    expect(payload.imap).toEqual({ tls: true, check_mailbox: 'INBOX', auth: { username: 'probe', password: 'pw' } })
    const form = soulFormDataFromSoul({ ...payload, id: 'x' } as Soul)
    expect(form).toMatchObject({ imapTLS: true, imapMailbox: 'INBOX', imapUsername: 'probe', imapPassword: 'pw' })
  })

  it('omits auth without a username', () => {
    const payload = buildSoulPayload({ ...defaultSoulFormData, name: 'Mail', type: 'imap', target: 'mail.example.com:143', imapTLS: false })
    expect(payload.imap).toEqual({ tls: false, check_mailbox: undefined, auth: undefined })
  })
})
