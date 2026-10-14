import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SoulDetail } from "./SoulDetail";

const mockNavigate = vi.fn();
const mockUpdateSoul = vi.fn();
const mockDeleteSoul = vi.fn();
const mockForceCheck = vi.fn();
const mockRefetchSoul = vi.fn();
const mockRefetchJudgments = vi.fn();
const mockUseSoul = vi.fn();
const mockUseSoulJudgments = vi.fn();
const mockUseSoulUptime = vi.fn();
const mockRefetchUptime = vi.fn();
const mockWriteText = vi.fn();

vi.mock("react-router-dom", async () => {
	const actual = await vi.importActual("react-router-dom");
	return {
		...actual,
		useNavigate: () => mockNavigate,
	};
});

vi.mock("../api/hooks", () => ({
	useSoul: () => mockUseSoul(),
	useSoulJudgments: () => mockUseSoulJudgments(),
	useSoulUptime: (...args: unknown[]) => mockUseSoulUptime(...args),
}));

type Window = { checks: number; up: number; failures: number; uptime_percent: number | null; avg_latency_ms: number };

function windowOf(checks: number, up: number, avg: number): Window {
	return { checks, up, failures: checks - up, uptime_percent: checks ? Math.round((up / checks) * 10000) / 100 : null, avg_latency_ms: checks ? avg : 0 };
}

// The server's uptime summary with a series of `points` equal buckets of
// `stepHours`, the last one containing now, oldest first.
function uptimeFixture(w24: Window, w7: Window, w30: Window, bucket: Window = windowOf(1, 1, 120), points = 7, stepHours = 24) {
	const step = stepHours * 3600 * 1000;
	const last = Math.floor(Date.now() / step) * step;
	const series = {
		range: "7d",
		step_seconds: stepHours * 3600,
		points: Array.from({ length: points }, (_, i) => ({ start: new Date(last - (points - 1 - i) * step).toISOString(), ...bucket })),
	};
	return { data: { soul_id: "soul-1", windows: { "24h": w24, "7d": w7, "30d": w30 }, series }, loading: false, error: null, refetch: mockRefetchUptime };
}

const noChecks = windowOf(0, 0, 0);

const soul = {
	id: "soul-1",
	name: "Payments API",
	type: "http",
	target: "https://payments.example.com/health",
	enabled: true,
	tags: ["critical"],
	created_at: "2026-07-01T00:00:00Z",
	updated_at: "2026-07-06T00:00:00Z",
};

const judgments = [
	{
		id: "j-1",
		status: "passed" as const,
		latency: 120,
		timestamp: "2026-07-06T10:00:00Z",
		region: "eu-west",
		purity: 98,
	},
	{
		id: "j-2",
		status: "failed" as const,
		latency: 1600,
		timestamp: "2026-07-06T09:00:00Z",
		region: "eu-west",
		purity: 30,
		error: "Timeout",
	},
];

function renderDetail() {
	render(
		<MemoryRouter initialEntries={["/souls/soul-1"]}>
			<Routes>
				<Route path="/souls/:id" element={<SoulDetail />} />
			</Routes>
		</MemoryRouter>,
	);
}

describe("SoulDetail", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText: mockWriteText },
			configurable: true,
		});
		Object.defineProperty(globalThis, "confirm", {
			value: vi.fn(() => true),
			configurable: true,
		});

		mockUseSoul.mockReturnValue({
			soul,
			loading: false,
			error: null,
			refetch: mockRefetchSoul,
			updateSoul: mockUpdateSoul,
			deleteSoul: mockDeleteSoul,
			forceCheck: mockForceCheck,
		});
		mockUseSoulJudgments.mockReturnValue({
			data: judgments,
			loading: false,
			error: null,
			refetch: mockRefetchJudgments,
		});
		mockUseSoulUptime.mockReturnValue(uptimeFixture(windowOf(2, 1, 860), windowOf(40, 38, 300), windowOf(200, 190, 280)));
		mockUpdateSoul.mockResolvedValue(undefined);
		mockDeleteSoul.mockResolvedValue(undefined);
		mockForceCheck.mockResolvedValue({ status: "passed", latency: 88 });
	});

	it("renders loading and not-found states", () => {
		mockUseSoul.mockReturnValueOnce({
			soul: null,
			loading: true,
			error: null,
			refetch: mockRefetchSoul,
			updateSoul: mockUpdateSoul,
			deleteSoul: mockDeleteSoul,
			forceCheck: mockForceCheck,
		});
		renderDetail();
		expect(document.querySelector(".animate-spin")).toBeInTheDocument();

		mockUseSoul.mockReturnValueOnce({
			soul: null,
			loading: false,
			error: "Soul not found",
			refetch: mockRefetchSoul,
			updateSoul: mockUpdateSoul,
			deleteSoul: mockDeleteSoul,
			forceCheck: mockForceCheck,
		});
		renderDetail();
		expect(screen.getByText("Soul not found")).toBeInTheDocument();
	});

	it("copies target, toggles enabled state, and navigates to edit/history tabs", async () => {
		renderDetail();

		expect(
			screen.getByRole("heading", { name: "Payments API" }),
		).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText("Copy target"));
		expect(mockWriteText).toHaveBeenCalledWith(
			"https://payments.example.com/health",
		);

		fireEvent.click(screen.getByRole("button", { name: /pause/i }));
		await waitFor(() => {
			expect(mockUpdateSoul).toHaveBeenCalledWith({ enabled: false });
		});

		fireEvent.click(screen.getByRole("button", { name: /edit/i }));
		expect(mockNavigate).toHaveBeenCalledWith("/souls/soul-1/edit");

		fireEvent.click(screen.getByRole("tab", { name: /history/i }));
		expect(screen.getByRole("tab", { name: /history/i })).toHaveAttribute(
			"aria-selected",
			"true",
		);
		expect(screen.getByText("Timeout")).toBeInTheDocument();

		fireEvent.click(screen.getByRole("tab", { name: /settings/i }));
		expect(screen.getByRole("tab", { name: /settings/i })).toHaveAttribute(
			"aria-selected",
			"true",
		);
	});

	it("runs a force check, refreshes data, and deletes after confirmation", async () => {
		renderDetail();

		fireEvent.click(screen.getByRole("button", { name: /test now/i }));
		await waitFor(() => {
			expect(mockForceCheck).toHaveBeenCalled();
		});
		await waitFor(() => {
			expect(mockRefetchSoul).toHaveBeenCalled();
			expect(mockRefetchJudgments).toHaveBeenCalled();
			expect(mockRefetchUptime).toHaveBeenCalled();
		});
		expect(
			screen.getByText(/Check passed! Latency: 88ms/i),
		).toBeInTheDocument();

		fireEvent.click(screen.getByLabelText("Delete soul"));
		// Confirm dialog should now be open — click the confirm button
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /confirm/i }));
		await waitFor(() => {
			expect(mockDeleteSoul).toHaveBeenCalled();
			expect(mockNavigate).toHaveBeenCalledWith("/souls");
		});
	});

	it("surfaces force-check failures and respects delete cancellation", async () => {
		mockForceCheck.mockRejectedValueOnce(new Error("network down"));
		renderDetail();

		fireEvent.click(screen.getByRole("button", { name: /test now/i }));
		await waitFor(() => {
			expect(
				screen.getByText(/Check failed: network down/i),
			).toBeInTheDocument();
		});

		fireEvent.click(screen.getByLabelText("Delete soul"));
		// Dialog opens — click Cancel to verify cancellation
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
		await waitFor(() => {
			expect(mockDeleteSoul).not.toHaveBeenCalled();
		});
	});

	it("renders judgment loading, error, and empty states", () => {
		mockUseSoulUptime.mockReturnValue(uptimeFixture(noChecks, noChecks, noChecks, noChecks));
		mockUseSoulJudgments.mockReturnValueOnce({ data: undefined, loading: true, error: null, refetch: mockRefetchJudgments });
		renderDetail();
		expect(document.querySelectorAll(".animate-spin").length).toBeGreaterThan(0);

		mockUseSoulJudgments.mockReturnValueOnce({ data: [], loading: false, error: "history unavailable", refetch: mockRefetchJudgments });
		renderDetail();
		expect(screen.getByText("history unavailable")).toBeInTheDocument();

		mockUseSoulJudgments.mockReturnValueOnce({ data: [], loading: false, error: null, refetch: mockRefetchJudgments });
		renderDetail();
		expect(screen.getByText("No judgments yet")).toBeInTheDocument();
		// Per render: the 24h uptime card and the failures card.
		expect(screen.getAllByText("No checks yet")).toHaveLength(6);
	});

	it("renders performance data, changes range, and navigates from secondary controls", () => {
		const today = new Date().toISOString();
		mockUseSoulJudgments.mockReturnValue({
			data: [...judgments, { id: "j-today", status: "pending", latency: 50, timestamp: today, purity: 50 }],
			loading: false, error: null, refetch: mockRefetchJudgments,
		});
		renderDetail();
		fireEvent.click(screen.getByRole("button", { name: "Souls" }));
		fireEvent.click(screen.getByLabelText("Back to souls"));
		fireEvent.click(screen.getByRole("button", { name: "View All" }));
		fireEvent.click(screen.getByRole("button", { name: "Global Judgments" }));
		expect(mockNavigate).toHaveBeenCalledWith("/judgments");
		fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
		fireEvent.change(screen.getByDisplayValue("Last 24 Hours"), { target: { value: "7d" } });
		expect(screen.getByDisplayValue("Last 7 Days")).toBeInTheDocument();
		expect(screen.getAllByTitle(/Response:/)).toHaveLength(7);
	});

	it("covers disabled metadata, configuration fallbacks, and settings actions", async () => {
		mockUseSoul.mockReturnValue({
			soul: { ...soul, enabled: false, tags: [], created_at: undefined, updated_at: undefined, interval: 30, weight: 60, timeout: 5, region: "us-east", http_config: { method: "POST" } },
			loading: false, error: null, refetch: mockRefetchSoul, updateSoul: mockUpdateSoul, deleteSoul: mockDeleteSoul, forceCheck: mockForceCheck,
		});
		renderDetail();
		expect(screen.getByText("No tags")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /resume/i }));
		await waitFor(() => expect(mockUpdateSoul).toHaveBeenCalledWith({ enabled: true }));
		fireEvent.click(screen.getByRole("tab", { name: /settings/i }));
		expect(screen.getAllByText("Unknown")).toHaveLength(2);
		expect(screen.getByText("POST")).toBeInTheDocument();
		fireEvent.click(screen.getAllByRole("button", { name: "Edit" }).at(-1)!);
		expect(mockNavigate).toHaveBeenCalledWith("/souls/soul-1/edit");
	});

	it.each([
		[undefined, "Check failed: Unknown error"],
		[{ status: "failed", error: undefined }, "Check failed: Unknown error"],
	])("handles non-passing force-check results %#", async (result, expected) => {
		mockForceCheck.mockResolvedValueOnce(result);
		renderDetail();
		fireEvent.click(screen.getByRole("button", { name: /test now/i }));
		expect(await screen.findByText(expected)).toBeInTheDocument();
	});

	it("reports non-Error check and delete failures", async () => {
		Object.defineProperty(globalThis, "alert", { value: vi.fn(), configurable: true });
		mockForceCheck.mockRejectedValueOnce("offline");
		mockDeleteSoul.mockRejectedValueOnce("delete failed");
		renderDetail();
		fireEvent.click(screen.getByRole("button", { name: /test now/i }));
		expect(await screen.findByText("Check failed: Unknown error")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText("Delete soul"));
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /confirm/i }));
		await waitFor(() => expect(globalThis.alert).toHaveBeenCalledWith("Failed to delete soul: Unknown error"));
	});

	it("uses not-found fallback navigation when no error message exists", () => {
		mockUseSoul.mockReturnValue({ soul: null, loading: false, error: null, refetch: mockRefetchSoul, updateSoul: mockUpdateSoul, deleteSoul: mockDeleteSoul, forceCheck: mockForceCheck });
		renderDetail();
		expect(screen.getByText("Soul not found")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "Back to Souls" }));
		expect(mockNavigate).toHaveBeenCalledWith("/souls");
	});

	it("renders unknown judgment defaults and the no-data performance state", () => {
		mockUseSoulJudgments.mockReturnValue({
			data: [{ id: "j-unknown", status: "mystery", latency: 0, timestamp: new Date().toISOString(), purity: 0 }],
			loading: false, error: null, refetch: mockRefetchJudgments,
		});
		mockUseSoulUptime.mockReturnValue(uptimeFixture(noChecks, noChecks, noChecks, noChecks));
		renderDetail();
		expect(screen.getByText("mystery")).toHaveClass("text-gray-400");
		fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
		expect(screen.getByText("No data available yet")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("tab", { name: /overview/i }));
	});

	it("renders fast and normal response labels and settings fallbacks", () => {
		mockUseSoul.mockReturnValue({
			soul: { ...soul, weight: 45, timeout: 4, region: undefined }, loading: false, error: null,
			refetch: mockRefetchSoul, updateSoul: mockUpdateSoul, deleteSoul: mockDeleteSoul, forceCheck: mockForceCheck,
		});
		mockUseSoulUptime.mockReturnValue(uptimeFixture(windowOf(1, 1, 50), windowOf(1, 1, 50), windowOf(1, 1, 50)));
		renderDetail();
		expect(screen.getByText("Fast")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("tab", { name: /settings/i }));
		expect(screen.getByText("45s")).toBeInTheDocument();
		expect(screen.getByText("global")).toBeInTheDocument();
	});

	// Uptime used to be computed from the judgments list, which holds only the
	// latest results of the last day: 7d and 30d covered at most a day, and a
	// soul with no checks showed 100%.
	it("shows the server's 24h, 7d and 30d uptime and 30-day totals", () => {
		renderDetail();
		expect(screen.getAllByText("50%").length).toBeGreaterThan(0); // 24h card and bar
		expect(screen.getAllByText("95%")).toHaveLength(2); // 7d and 30d bars (38/40, 190/200)
		expect(screen.getByText("860ms")).toBeInTheDocument(); // 24h average
		expect(screen.getByText("200")).toBeInTheDocument(); // checks in 30 days
		expect(screen.getByText("Last 30 days")).toBeInTheDocument();
		expect(screen.getByText("10")).toBeInTheDocument(); // failures in 30 days
	});

	it("shows no data instead of 100% for periods without checks", () => {
		mockUseSoulUptime.mockReturnValue(uptimeFixture(noChecks, windowOf(4, 4, 100), noChecks));
		renderDetail();
		expect(screen.getByText("—")).toBeInTheDocument();
		expect(screen.getAllByText("No checks")).toHaveLength(2); // 24h and 30d bars
		expect(screen.getAllByText("100%")).toHaveLength(1); // only the 7d bar
	});

	it("draws days without checks as empty, not as 100% uptime", () => {
		mockUseSoulUptime.mockReturnValue(uptimeFixture(windowOf(2, 2, 100), windowOf(2, 2, 100), windowOf(2, 2, 100), noChecks));
		const fixture = mockUseSoulUptime();
		fixture.data.series.points[6] = { ...fixture.data.series.points[6], ...windowOf(2, 2, 100) };
		mockUseSoulUptime.mockReturnValue(fixture);
		renderDetail();
		fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
		expect(screen.getAllByTitle("No checks")).toHaveLength(6);
		expect(screen.getAllByTitle("Uptime: 100%")).toHaveLength(1);
	});

	// The range selector used to change nothing: the chart always showed the
	// last 7 days whatever it said.
	it("requests and draws the range picked in the selector", () => {
		renderDetail();
		fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
		expect(mockUseSoulUptime).toHaveBeenLastCalledWith("soul-1", "24h");
		expect(screen.getByText("Performance trends over the last 24 hours")).toBeInTheDocument();

		mockUseSoulUptime.mockReturnValue(uptimeFixture(windowOf(2, 1, 860), windowOf(40, 38, 300), windowOf(200, 190, 280), windowOf(1, 1, 120), 30, 24));
		fireEvent.change(screen.getByDisplayValue("Last 24 Hours"), { target: { value: "30d" } });
		expect(mockUseSoulUptime).toHaveBeenLastCalledWith("soul-1", "30d");
		expect(screen.getByText("Performance trends over the last 30 days")).toBeInTheDocument();
		expect(screen.getAllByTitle(/Response:/)).toHaveLength(30);
		// Only every fourth of 30 bars is labelled, as month/day.
		const labels = screen.getAllByText(/^\d{1,2}\/\d{1,2}$/);
		expect(labels).toHaveLength(8);
	});
});
