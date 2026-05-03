import { test, expect } from '@playwright/test'
import type { Browser, Page } from '@playwright/test'
import { startServer, TestServer } from './server'

let server: TestServer

type CreatedSoul = {
  id: string
  name: string
  target: string
  type: string
}

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

async function createSoulViaUI(page: Page, soul: { name: string; type: string; target: string }): Promise<CreatedSoul> {
  await page.getByRole('button', { name: /Add Soul/i }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Add New Soul' })).toBeVisible()

  await dialog.getByPlaceholder('e.g., Production API').fill(soul.name)
  await dialog.locator('select').selectOption(soul.type)
  await dialog.getByPlaceholder('https://api.example.com/health').fill(soul.target)

  const createPromise = page.waitForResponse(
    (res) => res.url().endsWith('/api/v1/souls') && res.request().method() === 'POST'
  )
  await page.getByRole('button', { name: /Create Soul/i }).click()
  const createRes = await createPromise
  expect(createRes.status()).toBe(201)
  const createdSoul = await createRes.json() as CreatedSoul
  expect(createdSoul.id).toBeTruthy()
  expect(createdSoul.name).toBe(soul.name)
  expect(createdSoul.type).toBe(soul.type)
  expect(createdSoul.target).toBe(soul.target)

  await expect(page.getByRole('heading', { name: 'Add New Soul' })).not.toBeVisible()
  await expect(page.getByText(soul.name)).toBeVisible({ timeout: 10000 })
  return createdSoul
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

  test('login, create souls, and run immediate checks', async ({ page }) => {
    await loginAndOpenSouls(page)

    const runID = Date.now()
    const serverURL = new URL(server.baseURL)
    const monitorCases = [
      {
        name: `E2E HTTP Soul ${runID}`,
        type: 'http',
        target: `${server.baseURL}/health`,
      },
      {
        name: `E2E TCP Soul ${runID}`,
        type: 'tcp',
        target: `${serverURL.hostname}:${serverURL.port}`,
      },
    ]

    for (const monitor of monitorCases) {
      const createdSoul = await createSoulViaUI(page, monitor)

      await page.getByLabel(`View soul ${monitor.name}`).click()
      await expect(page.getByRole('heading', { name: monitor.name })).toBeVisible({ timeout: 10000 })

      const checkPromise = page.waitForResponse(
        (res) => res.url().endsWith(`/api/v1/souls/${createdSoul.id}/check`) && res.request().method() === 'POST'
      )
      await page.getByRole('button', { name: /Test Now/i }).click()
      const checkRes = await checkPromise
      expect(checkRes.status()).toBe(200)
      const judgment = await checkRes.json() as { soul_id: string; status: string }
      expect(judgment.soul_id).toBe(createdSoul.id)
      expect(['passed', 'failed', 'pending']).toContain(judgment.status)

      await expect(page.getByText(/Check passed|Check failed/i)).toBeVisible({ timeout: 10000 })
      await page.goto(`${server.baseURL}/souls`)
      await expect(page.getByRole('heading', { name: 'Souls', exact: true })).toBeVisible({ timeout: 10000 })
    }
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
    await createSoulViaUI(page, {
      name: soulName,
      type: 'http',
      target: `${server.baseURL}/health`,
    })

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
