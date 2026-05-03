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

type CreatedDashboard = {
  id: string
  name: string
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

function trackRuntimeIssues(page: Page) {
  const issues: string[] = []

  page.on('console', (message) => {
    if (message.type() === 'error') {
      const location = message.location()
      const source = location.url ? ` at ${location.url}` : ''
      issues.push(`console error: ${message.text()}${source}`)
    }
  })
  page.on('pageerror', (error) => {
    issues.push(`page error: ${error.message}`)
  })
  page.on('response', (response) => {
    const status = response.status()
    const resourceType = response.request().resourceType()
    if (status < 400 || resourceType === 'fetch' || resourceType === 'xhr') {
      return
    }
    issues.push(`resource ${status}: ${resourceType} ${response.url()}`)
  })

  return issues
}

function filterExpectedE2EIssues(issues: string[]) {
  const sawWebSocketRateLimit = issues.some((issue) =>
    issue.includes('/ws') && issue.includes('Unexpected response code: 429')
  )

  return issues.filter((issue) => {
    if (issue.includes('/ws') && issue.includes('Unexpected response code: 429')) {
      return false
    }
    if (sawWebSocketRateLimit && issue.startsWith('console error: WebSocket error: Event')) {
      return false
    }
    return true
  })
}

function authStorageState(token: string) {
  const serverURL = new URL(server.baseURL)

  return {
    cookies: [
      {
        name: 'auth_token',
        value: token,
        domain: serverURL.hostname,
        path: '/',
        expires: Math.floor(Date.now() / 1000) + 86400,
        httpOnly: true,
        secure: serverURL.protocol === 'https:',
        sameSite: 'Strict' as const,
      },
    ],
    origins: [
      {
        origin: serverURL.origin,
        localStorage: [{ name: 'auth_token', value: token }],
      },
    ],
  }
}

async function withAuthenticatedPage(
  browser: Browser,
  token: string,
  action: (page: Page) => Promise<void>
) {
  const context = await browser.newContext({
    serviceWorkers: 'block',
    storageState: authStorageState(token),
  })
  const page = await context.newPage()
  const issues = trackRuntimeIssues(page)

  try {
    await action(page)
    expect(filterExpectedE2EIssues(issues)).toEqual([])
  } finally {
    await context.close()
  }
}

async function createSoulViaAPI(page: Page, token: string, runID: number): Promise<CreatedSoul> {
  const createRes = await page.request.post(`${server.baseURL}/api/v1/souls`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name: `E2E Route Soul ${runID}`,
      type: 'http',
      target: `${server.baseURL}/health`,
      enabled: true,
      weight: '30s',
      timeout: '5s',
      http: { method: 'GET', valid_status: [200] },
    },
  })
  expect(createRes.status()).toBe(201)
  const created = await createRes.json() as CreatedSoul
  expect(created.id).toBeTruthy()
  return created
}

async function createDashboardViaAPI(page: Page, token: string, runID: number): Promise<CreatedDashboard> {
  const createRes = await page.request.post(`${server.baseURL}/api/v1/dashboards`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name: `E2E Dashboard ${runID}`,
      description: 'Route coverage dashboard',
      refresh_sec: 0,
      widgets: [
        {
          id: 'w_soul_count',
          title: 'Soul Count',
          type: 'stat',
          grid: { x: 0, y: 0, width: 4, height: 2 },
          query: { source: 'souls', metric: 'count', time_range: '24h' },
        },
      ],
    },
  })
  expect(createRes.status()).toBe(201)
  const created = await createRes.json() as CreatedDashboard
  expect(created.id).toBeTruthy()
  return created
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

async function expectRouteHeading(page: Page, path: string, headingName: string | RegExp) {
  const expectedURL = new URL(path, server.baseURL).toString()

  await page.goto(`${server.baseURL}${path}`, { waitUntil: 'domcontentloaded' })
  await expect(page).toHaveURL(expectedURL)
  await expect(page.getByRole('heading', { name: headingName }).first()).toBeVisible({ timeout: 30000 })
  await expect(page.locator('#main-content')).toBeVisible()
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

  test('renders every dashboard route and critical controls without runtime errors', async ({ browser }) => {
    const setupContext = await browser.newContext({ serviceWorkers: 'block' })
    const setupPage = await setupContext.newPage()
    const token = await authenticate(setupPage)
    const runID = Date.now()
    const createdSoul = await createSoulViaAPI(setupPage, token, runID)
    const createdDashboard = await createDashboardViaAPI(setupPage, token, runID)
    await setupContext.close()

    const routes = [
      ...lightModePages,
      { path: `/souls/${createdSoul.id}`, heading: createdSoul.name },
      { path: `/souls/${createdSoul.id}/edit`, heading: 'Edit Soul' },
      { path: '/dashboards/new', heading: 'New Dashboard' },
      { path: `/dashboards/${createdDashboard.id}`, heading: createdDashboard.name },
    ]

    for (const { path, heading } of routes) {
      await withAuthenticatedPage(browser, token, async (appPage) => {
        await expectRouteHeading(appPage, path, heading)
      })
    }

    await withAuthenticatedPage(browser, token, async (appPage) => {
      await appPage.goto(`${server.baseURL}/souls/${createdSoul.id}`)
      for (const tab of ['overview', 'performance', 'history', 'settings']) {
        await appPage.getByRole('tab', { name: tab }).click()
        await expect(appPage.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true')
      }
    })

    await withAuthenticatedPage(browser, token, async (appPage) => {
      await appPage.goto(`${server.baseURL}/alerts`)
      for (const tab of ['Alert Rules', 'Channels', 'History']) {
        await appPage.getByRole('tab', { name: new RegExp(tab) }).click()
        await expect(appPage.getByRole('tab', { name: new RegExp(tab) })).toHaveAttribute('aria-selected', 'true')
      }
    })

    await withAuthenticatedPage(browser, token, async (appPage) => {
      await appPage.goto(`${server.baseURL}/settings`)
      for (const tab of ['General', 'Security', 'Notifications', 'Storage', 'Integrations']) {
        await appPage.getByRole('tab', { name: new RegExp(tab) }).click()
        await expect(appPage.getByRole('tab', { name: new RegExp(tab) })).toHaveAttribute('aria-selected', 'true')
      }
    })

    const refreshControls = [
      { path: '/', label: 'Refresh dashboard' },
      { path: '/souls', label: 'Refresh souls' },
      { path: '/judgments', label: 'Refresh judgments' },
      { path: '/alerts', label: 'Refresh' },
      { path: '/maintenance', label: 'Refresh maintenance windows' },
      { path: '/journeys', label: 'Refresh journeys' },
      { path: '/cluster', label: 'Refresh cluster status' },
      { path: '/status-pages', label: 'Refresh status pages' },
      { path: `/dashboards/${createdDashboard.id}`, label: 'Refresh dashboard' },
      { path: '/settings', label: 'Refresh configuration' },
    ]

    for (const { path, label } of refreshControls) {
      await withAuthenticatedPage(browser, token, async (appPage) => {
        await appPage.goto(`${server.baseURL}${path}`)
        await appPage.getByRole('button', { name: label }).click()
        await expect(appPage.locator('#main-content')).toBeVisible()
      })
    }

    await withAuthenticatedPage(browser, token, async (appPage) => {
      await appPage.goto(`${server.baseURL}/dashboards/new`)
      const dashboardName = `E2E Created Dashboard ${runID}`
      await appPage.getByLabel('Name').fill(dashboardName)
      await appPage.getByLabel('Description').fill('Created from full dashboard route coverage')
      const createDashboardPromise = appPage.waitForResponse(
        (res) => res.url().endsWith('/api/v1/dashboards') && res.request().method() === 'POST'
      )
      await appPage.getByRole('button', { name: 'Create Dashboard' }).click()
      const createDashboardRes = await createDashboardPromise
      expect(createDashboardRes.status()).toBe(201)
      await expect(appPage.getByRole('heading', { name: dashboardName })).toBeVisible({ timeout: 10000 })
    })

    await withAuthenticatedPage(browser, token, async (appPage) => {
      await appPage.goto(`${server.baseURL}/dashboards/${createdDashboard.id}`)
      await appPage.getByRole('button', { name: 'Edit' }).click()
      await expect(appPage.getByRole('button', { name: 'Add Widget' })).toBeVisible()
      await appPage.getByRole('button', { name: 'Add Widget' }).click()
      await expect(appPage.getByRole('heading', { name: 'Add Widget' })).toBeVisible()
      await appPage.getByRole('button', { name: 'Cancel' }).click()
      await expect(appPage.getByRole('heading', { name: 'Add Widget' })).not.toBeVisible()
    })
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
      expect(['alive', 'dead', 'degraded', 'unknown', 'embalmed', 'passed', 'failed', 'pending']).toContain(judgment.status)

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
