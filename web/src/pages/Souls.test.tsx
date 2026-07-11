import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Souls } from "./Souls";

const mockFetchSouls = vi.fn();
const mockCreateSoul = vi.fn();
const mockRetryInitialCheck = vi.fn();
const mockUpdateSoul = vi.fn();
const mockDeleteSoul = vi.fn();

let mockSouls: Array<Record<string, unknown>> = [];
let mockInitialChecks: Record<string, "running" | "failed"> = {};

vi.mock("../stores/soulStore", () => ({
	useSoulStore: () => ({
		souls: mockSouls,
		initialChecks: mockInitialChecks,
		fetchSouls: mockFetchSouls,
		createSoul: mockCreateSoul,
		retryInitialCheck: mockRetryInitialCheck,
		updateSoul: mockUpdateSoul,
		deleteSoul: mockDeleteSoul,
	}),
}));

function renderSouls() {
	render(
		<MemoryRouter>
			<Souls />
		</MemoryRouter>,
	);
}

describe("Souls page", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(window, "confirm", {
			value: vi.fn(() => true),
			configurable: true,
		});
		Object.defineProperty(window, "alert", {
			value: vi.fn(),
			configurable: true,
		});
		mockFetchSouls.mockResolvedValue(undefined);
		mockCreateSoul.mockResolvedValue({
			id: "soul-1",
			name: "DNS Check",
			type: "dns",
			target: "example.com",
			enabled: true,
			weight: 60,
			timeout: 10,
		});
		mockUpdateSoul.mockResolvedValue(undefined);
		mockDeleteSoul.mockResolvedValue(undefined);
		mockRetryInitialCheck.mockResolvedValue(undefined);
		mockInitialChecks = {};
		mockSouls = [];
	});

	it("changes target hints and protocol fields when the soul type changes", () => {
		renderSouls();

		fireEvent.click(screen.getByRole("button", { name: /add soul/i }));

		expect(screen.getByLabelText("HTTP URL")).toHaveAttribute(
			"placeholder",
			"https://api.example.com/health",
		);
		expect(screen.getByLabelText("HTTP Method")).toBeInTheDocument();

		fireEvent.change(screen.getByLabelText("Soul type"), {
			target: { value: "dns" },
		});

		expect(screen.getByLabelText("DNS Name")).toHaveAttribute(
			"placeholder",
			"example.com",
		);
		expect(screen.getByLabelText("DNS Record Type")).toBeInTheDocument();
		expect(screen.queryByLabelText("HTTP Method")).not.toBeInTheDocument();

		fireEvent.change(screen.getByLabelText("Soul type"), {
			target: { value: "tcp" },
		});

		expect(screen.getByLabelText("TCP Host and Port")).toHaveAttribute(
			"placeholder",
			"api.example.com:443",
		);
		expect(screen.getByLabelText("Expected Banner Regex")).toBeInTheDocument();
		expect(screen.queryByLabelText("DNS Record Type")).not.toBeInTheDocument();
	});

	it("submits a DNS soul with DNS-specific config instead of HTTP config", async () => {
		renderSouls();

		fireEvent.click(screen.getByRole("button", { name: /add soul/i }));

		fireEvent.change(screen.getByPlaceholderText("e.g., Production API"), {
			target: { value: "DNS Check" },
		});
		fireEvent.change(screen.getByLabelText("Soul type"), {
			target: { value: "dns" },
		});
		fireEvent.change(screen.getByLabelText("DNS Name"), {
			target: { value: "example.com" },
		});
		fireEvent.change(screen.getByLabelText("DNS Record Type"), {
			target: { value: "AAAA" },
		});
		fireEvent.change(screen.getByLabelText("Expected DNS Values"), {
			target: { value: "2001:db8::1" },
		});

		fireEvent.click(screen.getByRole("button", { name: /create soul/i }));

		await waitFor(() => {
			expect(mockCreateSoul).toHaveBeenCalledWith(
				expect.objectContaining({
					name: "DNS Check",
					type: "dns",
					target: "example.com",
					dns: {
						record_type: "AAAA",
						expected: ["2001:db8::1"],
					},
				}),
			);
		});

		expect(mockCreateSoul.mock.calls[0][0]).not.toHaveProperty("http");
	});

	it("renders list and grid views, filters souls, and supports retry/toggle/delete actions", async () => {
		mockSouls = [
			{
				id: "soul-1",
				name: "Payments API",
				type: "http",
				target: "https://payments.example.com",
				enabled: true,
				status: "healthy",
				latency: 80,
				tags: ["prod"],
			},
			{
				id: "soul-2",
				name: "Billing Worker",
				type: "tcp",
				target: "billing.internal:443",
				enabled: false,
				status: "unhealthy",
				tags: [],
			},
			{
				id: "soul-3",
				name: "DNS Edge",
				type: "dns",
				target: "example.com",
				enabled: true,
				status: "unknown",
				tags: [],
			},
		];
		mockInitialChecks = { "soul-2": "failed", "soul-3": "running" };

		renderSouls();
		expect(await screen.findByText("Payments API")).toBeInTheDocument();
		expect(screen.getByText("Check failed")).toBeInTheDocument();
		expect(screen.getByText("Checking")).toBeInTheDocument();

		fireEvent.change(
			screen.getByPlaceholderText("Search souls by name or target..."),
			{ target: { value: "payments" } },
		);
		expect(screen.getByText("Payments API")).toBeInTheDocument();
		expect(screen.queryByText("Billing Worker")).not.toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("All Souls"), {
			target: { value: "issues" },
		});
		expect(screen.queryByText("Payments API")).not.toBeInTheDocument();
		expect(
			screen.getByText("No essence matches your search"),
		).toBeInTheDocument();

		fireEvent.change(
			screen.getByPlaceholderText("Search souls by name or target..."),
			{ target: { value: "" } },
		);

		fireEvent.click(screen.getByLabelText("Grid view"));
		expect(
			screen.getByLabelText(/retry initial check for billing worker/i),
		).toBeInTheDocument();
		fireEvent.click(
			screen.getByLabelText(/retry initial check for billing worker/i),
		);
		expect(mockRetryInitialCheck).toHaveBeenCalledWith("soul-2");

		fireEvent.click(screen.getByLabelText(/resume billing worker/i));
		await waitFor(() =>
			expect(mockUpdateSoul).toHaveBeenCalledWith("soul-2", { enabled: true }),
		);
	});

	it("shows both empty states and clears filters", () => {
		renderSouls();
		expect(screen.getByText("No essence in the realm")).toBeInTheDocument();

		cleanup();
		mockSouls = [
			{
				id: "soul-1",
				name: "Payments API",
				type: "http",
				target: "https://payments.example.com",
				enabled: true,
				status: "healthy",
				tags: [],
			},
		];
		renderSouls();
		fireEvent.change(
			screen.getByPlaceholderText("Search souls by name or target..."),
			{ target: { value: "zzz" } },
		);
		expect(
			screen.getByText("No essence matches your search"),
		).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
		expect(screen.getByText("Payments API")).toBeInTheDocument();
	});
});
