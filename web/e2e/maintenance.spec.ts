import { test, expect } from "@playwright/test";
import {
	type TestServer,
	startTestServer,
	trackRuntimeIssues,
	filterExpectedE2EIssues,
	authenticate,
} from "./helpers";

let server: TestServer;

test.beforeAll(async () => {
	server = await startTestServer();
});

test.afterAll(async () => {
	await server?.stop();
});

// ---------------------------------------------------------------------------
// Journey: Manage Maintenance Window
// Creates a window via API (datetime-local form inputs are brittle in
// Playwright), then verifies the full lifecycle through the Maintenance UI:
//   visible in list → toggle enable/disable → delete
// ---------------------------------------------------------------------------

const WINDOW_NAME = "E2E Database Migration";

test.describe("Manage Maintenance Window journey", () => {
	test("create, toggle, and delete a maintenance window", async ({
		page,
	}) => {
		const issues = trackRuntimeIssues(page);

		const token = await authenticate(page, server);
		const headers = { Authorization: `Bearer ${token}` };

		// --- Seed: create a maintenance window via API ---
		const now = new Date();
		const start = new Date(now.getTime() - 60_000); // started 1 min ago
		const end = new Date(now.getTime() + 3600_000); // ends in 1 hour

		const createRes = await page.context().request.post(
			`${server.baseURL}/api/v1/maintenance`,
			{
				headers,
				data: {
					name: WINDOW_NAME,
					description: "Planned DB migration for E2E",
					soul_ids: [],
					tags: [],
					start_time: start.toISOString(),
					end_time: end.toISOString(),
					recurring: "",
					enabled: true,
				},
			},
		);
		expect(createRes.status()).toBe(201);
		const window = await createRes.json();
		expect(window.id).toBeTruthy();

		// --- UI: verify the window appears in the list ---
		await page.goto(`${server.baseURL}/maintenance`);
		await page.getByText("Leave the Temple").waitFor({ timeout: 10_000 });

		// The window name should be visible in the list
		await expect(page.getByText(WINDOW_NAME).first()).toBeVisible({
			timeout: 10_000,
		});

		// --- UI: toggle the window off (disable) ---
		const toggle = page
			.getByRole("switch", {
				name: new RegExp(`Disable.*${WINDOW_NAME}`, "i"),
			})
			.first();
		await toggle.click();

		// Wait for the list to re-render
		await page.waitForTimeout(1500);

		// The toggle should now show "Enable" (window is disabled)
		await expect(
			page
				.getByRole("switch", {
					name: new RegExp(`Enable.*${WINDOW_NAME}`, "i"),
				})
				.first(),
		).toBeVisible({ timeout: 5_000 });

		// --- UI: delete the window ---
		// The ConfirmDialog is a React modal (not a native browser dialog),
		// so we must click the "Delete" button inside the modal after the
		// initial delete button opens it.
		const deleteBtn = page
			.getByRole("button", {
				name: new RegExp(`Delete.*${WINDOW_NAME}`, "i"),
			})
			.first();
		await deleteBtn.click();

		// Wait for the ConfirmDialog to appear, then click its confirm button.
		// The dialog has role="dialog" and the confirm button has default
		// label "Delete".
		const confirmDialog = page.getByRole("dialog", {
			name: /Delete Maintenance Window/i,
		});
		await expect(confirmDialog).toBeVisible({ timeout: 5_000 });
		await confirmDialog.getByRole("button", { name: /Confirm deletion/i }).click();

		// The window should be gone from the list. Scope to the list
		// container to avoid matching any lingering dialog text.
		await expect(
			page.locator(".space-y-3").getByText(WINDOW_NAME),
		).toHaveCount(0, { timeout: 10_000 });

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
