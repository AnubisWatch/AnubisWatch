import { test, expect } from "@playwright/test";
import {
	type TestServer,
	startTestServer,
	trackRuntimeIssues,
	filterExpectedE2EIssues,
} from "./helpers";

let server: TestServer;

test.beforeAll(async () => {
	server = await startTestServer();
});

test.afterAll(async () => {
	await server?.stop();
});

// ---------------------------------------------------------------------------
// Journey: Login / Logout
// Verifies the full auth round-trip through the real UI:
//   unauthenticated redirect → login form → submit → authenticated shell
//   → logout → back to login redirect
//
// Uses ID selectors (#login-email, #login-password) instead of getByLabel
// because getByLabel("Password") also matches the "Show password" toggle
// button's aria-label, causing strict-mode violations.
// ---------------------------------------------------------------------------

const EMAIL = "admin@anubis.watch";
const PASSWORD = "SecurePass123!";

async function fillLoginForm(page: import("@playwright/test").Page) {
	await page.locator("#login-email").fill(EMAIL);
	await page.locator("#login-password").fill(PASSWORD);
	await page.getByRole("button", { name: /Enter the Temple/i }).click();
}

test.describe("Login / Logout journey", () => {
	test("redirects unauthenticated users to /login", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await page.goto(`${server.baseURL}/souls`);

		await page.waitForURL("**/login", { timeout: 10_000 });
		// Verify the login form is rendered
		await expect(page.locator("#login-email")).toBeVisible();

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("login with valid credentials grants access", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await page.goto(`${server.baseURL}/login`);
		await expect(page.locator("#login-email")).toBeVisible();

		await fillLoginForm(page);

		await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });
		expect(page.url()).not.toContain("/login");

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("login with wrong credentials shows error", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await page.goto(`${server.baseURL}/login`);
		await expect(page.locator("#login-email")).toBeVisible();

		await page.locator("#login-email").fill(EMAIL);
		await page.locator("#login-password").fill("WrongPassword!");
		await page.getByRole("button", { name: /Enter the Temple/i }).click();

		// Should stay on login page
		await page.waitForTimeout(2000);
		expect(page.url()).toContain("/login");

		// An error message should appear (danger-colored text or banner)
		const errorBanner = page
			.locator(
				'[class*="red"], [class*="danger"], [class*="error"], [role="alert"]',
			)
			.first();
		await expect(errorBanner).toBeVisible({ timeout: 5_000 });

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("logout returns to login page", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await page.goto(`${server.baseURL}/login`);
		await fillLoginForm(page);
		await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });

		// Click the logout button in the sidebar
		await page.getByRole("button", { name: "Leave the Temple" }).click();

		await page.waitForURL("**/login", { timeout: 10_000 });
		await expect(page.locator("#login-email")).toBeVisible();

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
