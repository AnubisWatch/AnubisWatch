import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Settings } from "./Settings";

const settingsMocks = vi.hoisted(() => ({
	useAuth: vi.fn(),
	useStats: vi.fn(),
}));

const mockGet = vi.fn();
const mockPut = vi.fn();
const setTheme = vi.fn();
const applyTheme = vi.fn();
const writeText = vi.fn();

vi.mock("../api/hooks", async () => {
	const actual = await vi.importActual("../api/hooks");
	return {
		...actual,
		useAuth: settingsMocks.useAuth,
		useStats: settingsMocks.useStats,
	};
});

vi.mock("../api/client", () => ({
	api: {
		get: (...args: unknown[]) => mockGet(...(args as [string])),
		put: (...args: unknown[]) => mockPut(...(args as [string, unknown])),
	},
}));

vi.mock("../stores/themeStore", () => ({
	useThemeStore: () => ({ theme: "dark", setTheme }),
	applyTheme: (theme: string) => applyTheme(theme),
}));

const config = {
	instance_name: "AnubisWatch",
	timezone: "UTC",
	language: "en",
	theme: "dark" as const,
	retention_days: 30,
	storage_path: "/var/lib/anubis",
	auth_enabled: true,
	mcp_enabled: false,
	websocket_enabled: true,
};

describe("Settings", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		settingsMocks.useAuth.mockReturnValue({
			user: {
				id: "user-1",
				email: "admin@anubis.watch",
				role: "admin",
				workspace: "default",
			},
		});
		settingsMocks.useStats.mockReturnValue({
			data: {
				judgments: { today: 12 },
				souls: { total: 3 },
			},
		});
		mockGet.mockResolvedValue(config);
		mockPut.mockResolvedValue(undefined);
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText },
			configurable: true,
		});
		Object.defineProperty(window, "location", {
			value: {
				origin: "https://anubis.watch",
				protocol: "https:",
				host: "anubis.watch",
			},
			configurable: true,
		});
	});

	it("loads config, switches tabs, toggles settings, and saves changes", async () => {
		render(
			<MemoryRouter>
				<Settings />
			</MemoryRouter>,
		);

		await screen.findByText("Pharaoh's Chamber");
		expect(mockGet).toHaveBeenCalledWith("/config");

		fireEvent.click(screen.getByRole("tab", { name: /security/i }));
		fireEvent.click(screen.getAllByRole("switch")[0]);

		fireEvent.click(screen.getByRole("tab", { name: /notifications/i }));
		expect(screen.getByText("Email Notifications")).toBeInTheDocument();

		fireEvent.click(screen.getByRole("tab", { name: /storage/i }));
		fireEvent.change(screen.getByDisplayValue("/var/lib/anubis"), {
			target: { value: "/srv/anubis" },
		});

		fireEvent.click(screen.getByRole("tab", { name: /integrations/i }));
		fireEvent.click(screen.getByRole("button", { name: /show api key/i }));
		fireEvent.click(screen.getByRole("button", { name: /copy api key/i }));
		expect(writeText).toHaveBeenCalledWith("anb_live_user-1");
		expect(
			screen.getByDisplayValue("https://anubis.watch/mcp"),
		).toBeInTheDocument();
		expect(
			screen.getByDisplayValue("wss://anubis.watch/ws"),
		).toBeInTheDocument();

		fireEvent.click(screen.getByRole("tab", { name: /general/i }));
		fireEvent.change(screen.getByDisplayValue("AnubisWatch"), {
			target: { value: "Anubis Prime" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^Light$/i }));
		fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

		await waitFor(() => {
			expect(mockPut).toHaveBeenCalledWith(
				"/config",
				expect.objectContaining({
					instance_name: "Anubis Prime",
					storage_path: "/srv/anubis",
				}),
			);
		});
		expect(setTheme).toHaveBeenCalledWith("light");
		expect(applyTheme).toHaveBeenCalledWith("light");
	});

	it("shows save errors and keeps changes retryable", async () => {
		mockPut.mockRejectedValueOnce(new Error("save failed"));
		render(
			<MemoryRouter>
				<Settings />
			</MemoryRouter>,
		);

		await screen.findByDisplayValue("AnubisWatch");
		fireEvent.change(screen.getByDisplayValue("AnubisWatch"), {
			target: { value: "Anubis Prime" },
		});
		fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

		expect(await screen.findByText("save failed")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /save changes/i })).toBeEnabled();
	});

	it("shows error state and allows dismiss", async () => {
		mockGet.mockRejectedValueOnce(new Error("config failed"));

		render(
			<MemoryRouter>
				<Settings />
			</MemoryRouter>,
		);

		await screen.findByText("config failed");
		const errorBox = screen.getByText("config failed").closest("div");
		fireEvent.click(
			within(errorBox as HTMLElement).getByRole("button", {
				name: /dismiss error/i,
			}),
		);
		await waitFor(() => {
			expect(screen.queryByText("config failed")).not.toBeInTheDocument();
		});
	});
});
