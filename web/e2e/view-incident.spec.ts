import { test, expect } from "@playwright/test";
import {
	type TestServer,
	startTestServer,
	trackRuntimeIssues,
	filterExpectedE2EIssues,
	authenticate,
	createTestAlertChannel,
	waitForIncident,
} from "./helpers";

let server: TestServer;

test.beforeAll(async () => {
	server = await startTestServer();
});

test.afterAll(async () => {
	await server?.stop();
});

// ---------------------------------------------------------------------------
// Journey: View / Manage Incident
// Seeds an incident via the probe → alert pipeline (same as trigger-alert),
// then verifies the full incident lifecycle through the Incidents UI:
//   view list → acknowledge → resolve
// ---------------------------------------------------------------------------

const SOUL_NAME = "Incident Lifecycle Soul";
const SOUL_TARGET = "http://127.0.0.1:1"; // instant connection refused

test.describe("View Incident journey", () => {
	test("acknowledge and resolve an incident through the UI", async ({
		page,
	}) => {
		const issues = trackRuntimeIssues(page);

		// --- Seed: create channel + soul + rule to generate an incident ---
		const token = await authenticate(page, server);
		const headers = { Authorization: `Bearer ${token}` };

		const channelId = await createTestAlertChannel(page, server, token);

		const soulRes = await page.context().request.post(
			`${server.baseURL}/api/v1/souls`,
			{
				headers,
				data: {
					name: SOUL_NAME,
					type: "http",
					target: SOUL_TARGET,
					enabled: true,
					weight: "5s",
					timeout: "3s",
					http: { method: "GET", valid_status: [200] },
				},
			},
		);
		expect(soulRes.status()).toBe(201);
		const soul = await soulRes.json();

		const ruleRes = await page.context().request.post(
			`${server.baseURL}/api/v1/rules`,
			{
				headers,
				data: {
					name: "Incident Lifecycle Rule",
					enabled: true,
					severity: "critical",
					scope: {
						type: "specific",
						soul_ids: [soul.id],
					},
					conditions: [
						{
							type: "consecutive_failures",
							threshold: 2,
						},
					],
					channels: [channelId],
				},
			},
		);
		expect(ruleRes.status()).toBe(201);

		// --- Poll API until incident exists, then load the UI ---
		// The Incidents page fetches only on mount (no auto-refresh).
		await waitForIncident(page, server, token);

		await page.goto(`${server.baseURL}/incidents`);
		await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });

		await expect(page.getByText(SOUL_NAME).first()).toBeVisible({
			timeout: 10_000,
		});

		// --- Acknowledge ---
		await page
			.getByRole("button", { name: /Acknowledge/i })
			.first()
			.click();

		// The status should transition — wait for the page to re-render
		await page.waitForTimeout(2000);

		// --- Resolve ---
		await page.getByRole("button", { name: /Resolve/i }).first().click();

		// The status should transition to resolved
		await page.waitForTimeout(2000);

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
