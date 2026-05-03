import { test, expect } from '@playwright/test'
import type { Browser, Page } from '@playwright/test'
import { startServer, TestServer } from './server'

let server: TestServer

test.beforeAll(async () => {
  server = await startServer()
})

test.afterAll(async () => {
  await server?.stop()
})

async function authenticate(page: Page) {
  const loginRes = await page.request.post(`${server.baseURL}/api/v1/auth/login`, {
    data: {
      email: 'admin@anubis.watch',
      password: 'SecurePass123!',
    },
  })
  expect(loginRes.status()).toBe(200)
  const loginBody = await loginRes.json() as { token: string }
  expect(loginBody.token).toBeTruthy()
  return loginBody.token
}

async function installAuthToken(page: Page, token: string) {
  await page.addInitScript((token) => {
    localStorage.setItem('auth_token', token)
  }, token)
}

async function loginAndOpenSouls(page: Page) {
  const token = await authenticate(page)
  await installAuthToken(page, token)

  await page.goto(`${server.baseURL}/souls`)
  await page.waitForURL('**/souls')
  await expect(page.getByRole('heading', { name: 'Souls', exact: true })).toBeVisible({ timeout: 30000 })
}

async function createHttpSoul(page: Page, soulName: string) {
  await page.getByRole('button', { name: /Add Soul/i }).click()
  await expect(page.getByRole('heading', { name: 'Add New Soul' })).toBeVisible()

  await page.getByPlaceholder('e.g., Production API').fill(soulName)
  await page.getByPlaceholder('https://api.example.com/health').fill('https://example.com')

  const createPromise = page.waitForResponse(
    (res) => res.url().endsWith('/api/v1/souls') && res.request().method() === 'POST'
  )
  await page.getByRole('button', { name: /Create Soul/i }).click()
  const createRes = await createPromise
  expect(createRes.status()).toBe(201)

  await expect(page.getByRole('heading', { name: 'Add New Soul' })).not.toBeVisible()
  await expect(page.getByText(soulName)).toBeVisible({ timeout: 10000 })
}

const lightModePages = [
  { path: '/', heading: 'Hall of Judgment' },
  { path: '/souls', heading: 'Souls' },
  { path: '/judgments', heading: 'Judgments' },
  { path: '/alerts', heading: 'Alerts' },
  { path: '/incidents', heading: 'Incidents' },
  { path: '/maintenance', heading: 'Maintenance' },
  { path: '/journeys', heading: 'Journeys' },
  { path: '/cluster', heading: 'Cluster' },
  { path: '/status-pages', heading: 'Status Pages' },
  { path: '/dashboards', heading: 'Custom Dashboards' },
  { path: '/settings', heading: 'Settings' },
]

async function expectReadableLightPage(page: Page, path: string, headingName: string) {
  await page.locator(`nav a[href="${path}"]`).click()
  await expect(page).toHaveURL(new RegExp(path === '/' ? '/$' : `${path}$`))

  const root = page.locator('html')
  await expect(root).toHaveClass(/light/)
  await expect(root).toHaveCSS('color-scheme', 'light')

  const heading = page.getByRole('heading', { name: headingName, exact: true }).first()
  await expect(heading).toBeVisible({ timeout: 30000 })

  const styles = await heading.evaluate((element) => {
    const headingStyle = getComputedStyle(element)
    const bodyStyle = getComputedStyle(document.body)

    return {
      bodyBackground: bodyStyle.backgroundColor,
      bodyColor: bodyStyle.color,
      headingFontFamily: headingStyle.fontFamily,
    }
  })

  expect(styles.bodyBackground).toBe('rgb(249, 250, 251)')
  expect(styles.bodyColor).toBe('rgb(17, 24, 39)')
  expect(styles.headingFontFamily.toLowerCase()).not.toMatch(/cormorant|philosopher|cinzel/)
}

async function expectDirectlyLoadablePage(page: Page, path: string, headingName: string) {
  await page.goto(`${server.baseURL}${path}`, { waitUntil: 'domcontentloaded' })
  await expect(page).toHaveURL(new RegExp(path === '/' ? '/$' : `${path}$`))
  await expect(page.getByRole('heading', { name: headingName, exact: true }).first()).toBeVisible({ timeout: 30000 })
}

async function expectDirectlyLoadablePageInFreshContext(browser: Browser, token: string, path: string, headingName: string) {
  const context = await browser.newContext({ serviceWorkers: 'block' })
  await context.addInitScript((token) => {
    localStorage.setItem('auth_token', token)
  }, token)

  const page = await context.newPage()
  try {
    await expectDirectlyLoadablePage(page, path, headingName)
  } finally {
    await context.close()
  }
}

test.describe('AnubisWatch E2E Smoke', () => {
  test.describe.configure({ mode: 'serial' })
  test.setTimeout(120000)

  test('toggles light mode and preserves it after reload', async ({ page }) => {
    await loginAndOpenSouls(page)

    const root = page.locator('html')
    await expect(root).toHaveClass(/dark/)

    await page.getByLabel('Switch to light mode').click()
    await expect(root).toHaveClass(/light/)
    await expect(root).not.toHaveClass(/dark/)
    await expect(root).toHaveCSS('color-scheme', 'light')
    await expect(page.getByLabel('Switch to dark mode')).toBeVisible()

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Souls', exact: true })).toBeVisible({ timeout: 10000 })
    await expect(root).toHaveClass(/light/)
    await expect(root).toHaveCSS('color-scheme', 'light')
  })

  test('keeps primary pages readable in light mode', async ({ page }) => {
    await loginAndOpenSouls(page)

    await page.getByLabel('Switch to light mode').click()

    for (const { path, heading } of lightModePages) {
      await expectReadableLightPage(page, path, heading)
    }
  })

  test('supports direct reloads for primary app routes', async ({ browser, page }) => {
    const token = await authenticate(page)

    for (const { path, heading } of lightModePages) {
      await expectDirectlyLoadablePageInFreshContext(browser, token, path, heading)
    }
  })

  test('login, create soul, and run an immediate check', async ({ page }) => {
    await loginAndOpenSouls(page)

    const soulName = `E2E Smoke Soul ${Date.now()}`
    await createHttpSoul(page, soulName)

    await page.getByLabel(`View soul ${soulName}`).click()
    await expect(page.getByRole('heading', { name: soulName })).toBeVisible({ timeout: 10000 })

    const checkPromise = page.waitForResponse(
      (res) => res.url().includes('/check') && res.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /Test Now/i }).click()
    const checkRes = await checkPromise
    expect(checkRes.status()).toBe(200)

    await expect(page.getByText(/Check passed|Check failed/i)).toBeVisible({ timeout: 10000 })
  })

  test('shows retry when the automatic initial check request fails', async ({ page }) => {
    await loginAndOpenSouls(page)

    let failNextCheck = true
    await page.route('**/api/v1/souls/*/check', async (route) => {
      if (failNextCheck) {
        failNextCheck = false
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'forced e2e initial check failure' }),
        })
        return
      }

      await route.continue()
    })

    const soulName = `E2E Retry Soul ${Date.now()}`
    await createHttpSoul(page, soulName)

    const retryButton = page.getByLabel(`Retry initial check for ${soulName}`)
    await expect(retryButton).toBeVisible({ timeout: 10000 })

    const retryPromise = page.waitForResponse(
      (res) => res.url().includes('/check') && res.request().method() === 'POST' && res.status() === 200
    )
    await retryButton.click()
    await retryPromise

    await expect(retryButton).not.toBeVisible({ timeout: 10000 })
    const soulRow = page.getByRole('row').filter({ hasText: soulName })
    await expect(soulRow.getByText('Check failed')).not.toBeVisible()
    await expect(soulRow.getByText(/Healthy|Unhealthy/)).toBeVisible({ timeout: 10000 })
  })
})
