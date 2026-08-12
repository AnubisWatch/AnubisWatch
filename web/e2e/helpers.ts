import type { Page } from "@playwright/test";
import { startServer, type TestServer } from "./server";

export type { TestServer };

export const ADMIN_EMAIL = "admin@anubis.watch";
export const ADMIN_PASSWORD = "SecurePass123!";

/**
 * Starts a fresh isolated server instance for a spec file.
 * Each spec file calls this in `test.beforeAll` to get its own
 * server + data dir, preventing cross-test-state leakage.
 */
export async function startTestServer(): Promise<TestServer> {
	return startServer();
}

/**
 * Authenticates via the browser context's request API so the
 * httpOnly `auth_token` cookie is set on the browser context itself.
 * This mirrors the smoke test's authenticate() helper.
 */
export async function authenticate(
	page: Page,
	server: TestServer,
): Promise<string> {
	const ctx = page.context();
	const loginRes = await ctx.request.post(
		`${server.baseURL}/api/v1/auth/login`,
		{
			data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
		},
	);
	if (loginRes.status() !== 200) {
		throw new Error(
			`Login failed: ${loginRes.status()} ${await loginRes.text()}`,
		);
	}
	const cookies = await ctx.cookies(server.baseURL);
	const cookie = cookies.find((entry) => entry.name === "auth_token");
	if (!cookie?.value) {
		throw new Error("auth_token cookie not set after login");
	}
	return cookie.value;
}

/**
 * Authenticates and navigates the page to the given path,
 * waiting for the main shell to render (sidebar "Leave the Temple").
 */
export async function loginAndNavigate(
	page: Page,
	server: TestServer,
	path = "/",
): Promise<void> {
	await authenticate(page, server);
	await page.goto(`${server.baseURL}${path}`);
	await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });
}

/**
 * Tracks console errors and page errors for runtime issue detection.
 * Call with expect(filterExpectedE2EIssues(issues)).toEqual([]) at end of test.
 */
export function trackRuntimeIssues(page: Page): string[] {
	const issues: string[] = [];

	page.on("console", (message) => {
		if (message.type() === "error") {
			issues.push(`console error: ${message.text()}`);
		}
	});
	page.on("pageerror", (error) => {
		issues.push(`page error: ${error.message}`);
	});

	return issues;
}

/**
 * Filters out known-acceptable runtime issues (e.g., WebSocket rate-limit
 * during parallel E2E runs).
 */
/**
 * Create a minimal alert channel via the API so it can be referenced in
 * rules (rule validation requires at least one channel).
 */
export async function createTestAlertChannel(
	page: import("@playwright/test").Page,
	server: { baseURL: string },
	token: string,
	name = "E2E Test Channel",
): Promise<string> {
	const res = await page.context().request.post(
		`${server.baseURL}/api/v1/channels`,
		{
			headers: { Authorization: `Bearer ${token}` },
			data: {
				name,
				type: "webhook",
				enabled: true,
				config: { url: "http://127.0.0.1:1/hook" },
			},
		},
	);
	if (res.status() !== 201) {
		throw new Error(
			`Failed to create test channel: ${res.status()} ${await res.text()}`,
		);
	}
	const channel = await res.json();
	return channel.id;
}

/**
 * Polls the incidents API until at least one incident exists (or the timeout
 * is reached). The Incidents page fetches only on mount (no polling), so we
 * must ensure an incident exists before navigating to it.
 */
export async function waitForIncident(
	page: import("@playwright/test").Page,
	server: { baseURL: string },
	token: string,
	timeoutMs = 90_000,
): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		const res = await page.context().request.get(
			`${server.baseURL}/api/v1/incidents`,
			{ headers: { Authorization: `Bearer ${token}` } },
		);
		if (res.status() === 200) {
			const incidents = await res.json();
			if (Array.isArray(incidents) && incidents.length > 0) return;
		}
		await page.waitForTimeout(3_000);
	}
	throw new Error(`No incidents created within ${timeoutMs}ms`);
}

export function filterExpectedE2EIssues(issues: string[]): string[] {
	return issues.filter((issue) => {
		if (issue.includes("/ws") && issue.includes("429")) return false;
		if (
			issue.includes("/ws") &&
			issue.startsWith("console error: WebSocket error")
		)
			return false;
		// 401 on API routes is expected when testing unauthenticated flows
		// (redirect-to-login, wrong-credentials, logout).
		if (issue.includes("401 (Unauthorized)")) return false;
		return true;
	});
}
