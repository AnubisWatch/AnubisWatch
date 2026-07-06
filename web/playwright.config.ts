import fs from "node:fs";
import { defineConfig, devices } from "@playwright/test";

const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const fallbackExecutablePath = [
	chromiumExecutablePath,
	"/usr/bin/google-chrome",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/chromium",
	"/usr/bin/chromium-browser",
].find((candidate) => candidate && fs.existsSync(candidate));

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: process.env.CI ? 1 : undefined,
	reporter: "list",
	use: {
		baseURL: process.env.E2E_BASE_URL || "http://localhost:18080",
		serviceWorkers: "block",
		trace: "on-first-retry",
	},
	projects: [
		{
			name: "chromium",
			use: {
				...devices["Desktop Chrome"],
				launchOptions: fallbackExecutablePath
					? { executablePath: fallbackExecutablePath }
					: undefined,
			},
		},
	],
});
