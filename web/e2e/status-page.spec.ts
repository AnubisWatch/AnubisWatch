import { test, expect, type Page } from "@playwright/test";
import {
	startTestServer,
	authenticate,
	loginAndNavigate,
	trackRuntimeIssues,
	filterExpectedE2EIssues,
	type TestServer,
} from "./helpers";

test.describe.serial("Status Page journey", () => {
	let server: TestServer;

	test.beforeAll(async () => {
		server = await startTestServer();
	});
	test.afterAll(async () => {
		await server.stop();
	});

	const PAGE_NAME = "E2E Public Status";
	const PAGE_SLUG = "e2e-public-status";
	const PAGE_DESC = "Created by the status-page E2E spec";
	const SOUL_NAME = "E2E Status Soul";

	/**
	 * Creates a minimal HTTP soul via the API so it appears in the
	 * "Linked Services" picker on the status page form.
	 * Returns the soul ID.
	 */
	async function createSoulForStatus(
		page: Page,
		token: string,
	): Promise<string> {
		const res = await page.context().request.post(
			`${server.baseURL}/api/v1/souls`,
			{
				headers: { Authorization: `Bearer ${token}` },
				data: {
					name: SOUL_NAME,
					type: "http",
					target: "http://127.0.0.1:1/",
					enabled: true,
					weight: "30s",
					timeout: "5s",
					http: { method: "GET", valid_status: [200] },
				},
			},
		);
		if (res.status() !== 201) {
			throw new Error(
				`Soul creation failed: ${res.status()} ${await res.text()}`,
			);
		}
		const soul = await res.json();
		return soul.id;
	}

	/**
	 * Deletes a status page via the API (cleanup helper).
	 */
	async function deleteStatusPage(page: Page, token: string, id: string) {
		await page.context().request.delete(
			`${server.baseURL}/api/v1/status-pages/${id}`,
			{ headers: { Authorization: `Bearer ${token}` } },
		);
	}

	test("create a status page with a linked soul and view it publicly", async ({
		page,
	}) => {
		const issues = trackRuntimeIssues(page);
		const token = await authenticate(page, server);
		const soulId = await createSoulForStatus(page, token);

		// --- Navigate to Status Pages management UI ---
		await loginAndNavigate(page, server, "/status-pages");
		await expect(
			page.getByRole("heading", { name: "Temple Squares" }),
		).toBeVisible();

		// --- Open the create modal ---
		await page.getByRole("button", { name: "Create Page" }).first().click();
		await expect(page.getByText("Create Status Page").first()).toBeVisible();

		// --- Fill the form ---
		await page.locator("#status-page-name").fill(PAGE_NAME);
		await page.locator("#status-page-slug").fill(PAGE_SLUG);
		await page.locator("#status-page-description").fill(PAGE_DESC);

		// Select Light theme (Dark is default; click Light to exercise the picker)
		await page.getByRole("button", { name: "Light", exact: true }).click();

		// Link the soul — click the soul entry in the "Linked Services" list
		const soulButton = page.getByRole("button", { name: SOUL_NAME });
		await soulButton.click();

		// --- Submit ---
		await page
			.getByRole("button", { name: "Create Status Page", exact: true })
			.click();

		// --- Verify the page appears in the management grid ---
		await expect(page.getByText(PAGE_NAME).first()).toBeVisible({
			timeout: 10_000,
		});
		// The slug-based URL should be visible
		await expect(page.getByText(`/status/${PAGE_SLUG}`).first()).toBeVisible();
		// Active badge (enabled by default on create)
		await expect(page.getByText("Active").first()).toBeVisible();

		// --- View the public status page (server-rendered HTML) ---
		await page.goto(`${server.baseURL}/status/${PAGE_SLUG}`);

		// The public page renders the soul name and an overall status indicator.
		// These are Go-generated HTML elements with class-based selectors
		// (see internal/statuspage/handler.go renderStatusPage).
		await expect(page.locator(".soul-name", { hasText: SOUL_NAME })).toBeVisible({
			timeout: 10_000,
		});
		// The page title area should contain the status page name
		await expect(page.getByText(PAGE_NAME)).toBeVisible();

		// --- Cleanup ---
		await deleteStatusPage(page, token, soulId);

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("form validation prevents empty submission", async ({ page }) => {
		const issues = trackRuntimeIssues(page);
		await loginAndNavigate(page, server, "/status-pages");

		await page.getByRole("button", { name: "Create Page" }).first().click();
		await expect(page.getByText("Create Status Page").first()).toBeVisible();

		// The submit button should be disabled when name is empty
		const submitBtn = page.getByRole("button", { name: "Create Status Page", exact: true });
		await expect(submitBtn).toBeDisabled();

		// Typing only a slug (no name) should still be disabled
		await page.locator("#status-page-slug").fill("has-slug");
		await expect(submitBtn).toBeDisabled();

		// Typing a name auto-generates the slug, enabling the button
		await page.locator("#status-page-name").fill("Has Name");
		await expect(submitBtn).toBeEnabled();

		// Close the modal without saving
		await page.getByRole("button", { name: "Close dialog" }).click();

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
