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
// Journey: Trigger Alert
// Creates a soul targeting a dead endpoint, attaches a consecutive-failure
// rule (with a channel, as rule validation requires), and waits for an
// incident to surface on the Incidents page.
//
// Setup (soul + channel + rule) uses the API directly because the
// timing-critical part is the probe-engine → alert-engine → incident
// pipeline. Verification is done through the real Incidents UI.
// ---------------------------------------------------------------------------

const SOUL_NAME = "Alert Trigger Soul";
const SOUL_TARGET = "http://127.0.0.1:1"; // port 1 — instant connection refused

test.describe("Trigger Alert journey", () => {
	test("probe failures create an incident visible in the UI", async ({
		page,
	}) => {
		const issues = trackRuntimeIssues(page);

		// --- API setup: create channel + soul + rule ---
		const token = await authenticate(page, server);
		const headers = { Authorization: `Bearer ${token}` };

		// Rule validation requires at least one channel — create one first.
		const channelId = await createTestAlertChannel(page, server, token);

		// Create a soul with a short interval so failures accumulate fast.
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

		// Create a rule scoped to this soul that fires after 2 failures.
		const ruleRes = await page.context().request.post(
			`${server.baseURL}/api/v1/rules`,
			{
				headers,
				data: {
					name: "E2E Alert Rule",
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

		// --- Poll the API until the incident exists, then load the UI ---
		// The Incidents page fetches only on mount (no auto-refresh), so we
		// must wait for the incident to exist before navigating.
		await waitForIncident(page, server, token);

		await page.goto(`${server.baseURL}/incidents`);
		await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });

		// The incident should now be visible on the page.
		await expect(page.getByText(SOUL_NAME).first()).toBeVisible({
			timeout: 10_000,
		});

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
