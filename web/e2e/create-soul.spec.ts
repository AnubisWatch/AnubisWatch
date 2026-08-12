import { test, expect } from "@playwright/test";
import {
	type TestServer,
	startTestServer,
	trackRuntimeIssues,
	filterExpectedE2EIssues,
	loginAndNavigate,
} from "./helpers";

let server: TestServer;

test.beforeAll(async () => {
	server = await startTestServer();
});

test.afterAll(async () => {
	await server?.stop();
});

// ---------------------------------------------------------------------------
// Journey: Create Soul
// Verifies the full Soul creation flow through the UI:
//   navigate to Souls → open modal → fill form → submit → verify in list
// ---------------------------------------------------------------------------

const SOUL_NAME = `E2E Test Soul`;
const SOUL_TARGET = "https://httpbin.org/status/200";

test.describe.serial("Create Soul journey", () => {
	test("create a new HTTP soul via the modal", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await loginAndNavigate(page, server, "/souls");

		// Click the "Add Soul" button to open the modal
		const addButton = page
			.getByRole("button", { name: /Add.*Soul|Create.*Soul/i })
			.first();
		await addButton.waitFor({ timeout: 10_000 });
		await addButton.click();

		// Wait for modal
		await expect(
			page.getByRole("heading", { name: "Add New Soul" }),
		).toBeVisible({ timeout: 5_000 });

		// Fill the name field
		await page.getByPlaceholder("e.g., Production API").fill(SOUL_NAME);

		// Select type = http (should be default, but be explicit)
		await page.getByLabel("Soul type").selectOption("http");

		// Fill target URL
		await page.getByTestId("soul-target").fill(SOUL_TARGET);

		// Submit
		await page.getByRole("button", { name: /Create Soul/i }).click();

		// Wait for the soul to appear in the list
		await expect(page.getByText(SOUL_NAME)).toBeVisible({
			timeout: 10_000,
		});

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("created soul persists after page reload", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await loginAndNavigate(page, server, "/souls");

		// Verify the soul from the previous test is still there
		await expect(page.getByText(SOUL_NAME)).toBeVisible({
			timeout: 10_000,
		});

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});

	test("form validation prevents empty submission", async ({ page }) => {
		const issues = trackRuntimeIssues(page);

		await loginAndNavigate(page, server, "/souls");

		await page
			.getByRole("button", { name: /Add.*Soul|Create.*Soul/i })
			.first()
			.click();

		await expect(
			page.getByRole("heading", { name: "Add New Soul" }),
		).toBeVisible({ timeout: 5_000 });

		// The form has `required` attributes — browser should block submission
		await page.getByRole("button", { name: /Create Soul/i }).click();

		// Modal should still be open
		await expect(
			page.getByRole("heading", { name: "Add New Soul" }),
		).toBeVisible();

		expect(filterExpectedE2EIssues(issues)).toEqual([]);
	});
});
