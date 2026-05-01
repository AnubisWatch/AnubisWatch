import { test, expect } from '@playwright/test'
import { startServer, TestServer } from './server'

let server: TestServer

test.beforeAll(async () => {
  server = await startServer()
})

test.afterAll(async () => {
  await server?.stop()
})

test.describe('AnubisWatch E2E Smoke', () => {
  test('login, create soul, and run an immediate check', async ({ page }) => {
    // 1. Navigate to login
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: /Anubis/i })).toBeVisible()

    // 2. Log in with demo credentials
    await page.getByPlaceholder('priest@anubis.watch').fill('admin@anubis.watch')
    await page.getByPlaceholder('••••••••').fill('SecurePass123!')
    await page.getByRole('button', { name: /Enter the Temple/i }).click()

    // 3. Wait for dashboard redirect
    await page.waitForURL('**/')

    // 4. Navigate to Souls page and wait for spinner to disappear
    await page.goto('/souls')
    await expect(page.getByRole('heading', { name: 'Souls', exact: true })).toBeVisible({ timeout: 10000 })

    // 5. Open Add Soul modal
    await page.getByRole('button', { name: /Add Soul/i }).click()
    await expect(page.getByRole('heading', { name: 'Add New Soul' })).toBeVisible()

    // 6. Fill in the form
    const soulName = `E2E Smoke Soul ${Date.now()}`
    await page.getByPlaceholder('e.g., Production API').fill(soulName)
    await page.getByPlaceholder('https://api.example.com/health').fill('https://example.com')

    // 7. Submit and wait for POST response
    const createPromise = page.waitForResponse(
      (res) => res.url().includes('/souls') && res.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /Create Soul/i }).click()
    const createRes = await createPromise
    expect(createRes.status()).toBe(201)

    // 8. Verify modal closes and soul appears in list
    await expect(page.getByRole('heading', { name: 'Add New Soul' })).not.toBeVisible()
    await expect(page.getByText(soulName)).toBeVisible({ timeout: 10000 })

    // 9. Open details and run an immediate check
    await page.getByLabel(`Edit soul ${soulName}`).click()
    await expect(page.getByRole('heading', { name: soulName })).toBeVisible({ timeout: 10000 })

    const checkPromise = page.waitForResponse(
      (res) => res.url().includes('/check') && res.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /Test Now/i }).click()
    const checkRes = await checkPromise
    expect(checkRes.status()).toBe(200)

    await expect(page.getByText(/Check passed|Check failed/i)).toBeVisible({ timeout: 10000 })
  })
})
