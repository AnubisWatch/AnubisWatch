import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";

const dashboardMocks = vi.hoisted(() => ({
	useSouls: vi.fn(),
	useStats: vi.fn(),
	useClusterStatus: vi.fn(),
	useJudgments: vi.fn(),
}));

const printMock = vi.fn();

vi.mock("../api/hooks", async () => {
	const actual = await vi.importActual("../api/hooks");
	return {
		...actual,
		useSouls: dashboardMocks.useSouls,
		useStats: dashboardMocks.useStats,
		useClusterStatus: dashboardMocks.useClusterStatus,
		useJudgments: dashboardMocks.useJudgments,
	};
});

vi.mock("../components/EventsFeed", () => ({
	EventsFeed: () => <div>Live feed</div>,
}));

describe("Dashboard", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(window, "print", {
			value: printMock,
			configurable: true,
		});
		dashboardMocks.useSouls.mockReturnValue({
			souls: [
				{
					id: "soul-1",
					name: "Payments API",
					target: "https://payments.example.com",
					enabled: true,
					status: "healthy",
				},
				{
					id: "soul-2",
					name: "Billing Worker",
					target: "https://billing.example.com",
					enabled: false,
					status: "unhealthy",
				},
			],
			refetch: vi.fn().mockResolvedValue(undefined),
		});
		dashboardMocks.useStats.mockReturnValue({
			data: {
				souls: { total: 2, healthy: 1, degraded: 0, dead: 1 },
				judgments: { today: 12, failures: 1, avg_latency_ms: 140 },
				alerts: { channels: 2, rules: 1, active_incidents: 1 },
			},
			refetch: vi.fn().mockResolvedValue(undefined),
		});
		dashboardMocks.useClusterStatus.mockReturnValue({
			data: { is_clustered: true, peer_count: 3 },
		});
		dashboardMocks.useJudgments.mockReturnValue({
			data: [
				{
					id: "j-1",
					status: "passed",
					latency: 100,
					timestamp: "2026-07-06T10:00:00Z",
				},
				{
					id: "j-2",
					status: "failed",
					latency: 400,
					timestamp: "2026-07-06T10:30:00Z",
				},
			],
		});
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("renders stats, charts, system status, recent souls, and quick actions", () => {
		render(
			<MemoryRouter>
				<Dashboard />
			</MemoryRouter>,
		);

		expect(
			screen.getByRole("heading", { name: "Hall of Judgment" }),
		).toBeInTheDocument();
		expect(screen.getByText("Total Souls")).toBeInTheDocument();
		expect(screen.getByText("Pure Hearts")).toBeInTheDocument();
		expect(screen.getByText("Chaos")).toBeInTheDocument();
		expect(screen.getByText("Balance")).toBeInTheDocument();
		expect(screen.getByText("Activity Overview")).toBeInTheDocument();
		expect(screen.getByText("System Status")).toBeInTheDocument();
		expect(screen.getByText("Recent Souls")).toBeInTheDocument();
		expect(screen.getByText("Quick Actions")).toBeInTheDocument();
		expect(screen.getByText("Live feed")).toBeInTheDocument();
		expect(screen.getByRole("link", { name: /Payments API/i })).toHaveAttribute(
			"href",
			"/souls/soul-1",
		);
		expect(
			screen.getByRole("link", { name: /Billing Worker/i }),
		).toHaveAttribute("href", "/souls/soul-2");
		expect(screen.getByRole("link", { name: /Add Soul/i })).toHaveAttribute(
			"href",
			"/souls",
		);
		expect(screen.getByRole("link", { name: /Alerts/i })).toHaveAttribute(
			"href",
			"/alerts",
		);
	});

	it("refreshes and exports from the header actions", async () => {
		const refetchSouls = vi.fn().mockResolvedValue(undefined);
		const refetchStats = vi.fn().mockResolvedValue(undefined);
		dashboardMocks.useSouls.mockReturnValue({
			souls: [],
			refetch: refetchSouls,
		});
		dashboardMocks.useStats.mockReturnValue({
			data: {
				souls: { total: 0, healthy: 0, degraded: 0, dead: 0 },
				judgments: { today: 0, failures: 0, avg_latency_ms: 0 },
				alerts: { channels: 0, rules: 0, active_incidents: 0 },
			},
			refetch: refetchStats,
		});
		dashboardMocks.useClusterStatus.mockReturnValue({
			data: { is_clustered: false, peer_count: 0 },
		});
		dashboardMocks.useJudgments.mockReturnValue({ data: [] });

		render(
			<MemoryRouter>
				<Dashboard />
			</MemoryRouter>,
		);

		fireEvent.click(screen.getByLabelText("Export dashboard as PDF"));
		expect(printMock).toHaveBeenCalled();

		fireEvent.click(screen.getByLabelText("Refresh dashboard"));
		await waitFor(() => {
			expect(refetchSouls).toHaveBeenCalled();
			expect(refetchStats).toHaveBeenCalled();
		});
	});

	it("handles invalid soul data, absent stats, and completes the refresh timer", async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		const refetchSouls = vi.fn().mockResolvedValue(undefined);
		const refetchStats = vi.fn().mockResolvedValue(undefined);
		dashboardMocks.useSouls.mockReturnValue({ souls: { slice: () => [] }, refetch: refetchSouls });
		dashboardMocks.useStats.mockReturnValue({ data: undefined, refetch: refetchStats });
		dashboardMocks.useClusterStatus.mockReturnValue({ data: undefined });
		dashboardMocks.useJudgments.mockReturnValue({ data: undefined });
		render(<MemoryRouter><Dashboard /></MemoryRouter>);
		expect(screen.getByText("Start Monitoring")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText("Refresh dashboard"));
		await waitFor(() => expect(refetchSouls).toHaveBeenCalledOnce());
		await act(async () => {
			await vi.advanceTimersByTimeAsync(500);
		});
		await waitFor(() => expect(screen.getByLabelText("Refresh dashboard")).not.toHaveClass("animate-spin"));
	});

	it("aggregates judgments inside and outside the current hour", () => {
		vi.setSystemTime(new Date("2026-07-06T10:30:00Z"));
		dashboardMocks.useJudgments.mockReturnValue({ data: [
			{ id: "j1", status: "passed", latency: 100, timestamp: "2026-07-06T10:05:00Z" },
			{ id: "j2", status: "failed", latency: 300, timestamp: "2026-07-06T10:20:00Z" },
			{ id: "j3", status: "passed", latency: 50, timestamp: "2026-07-05T00:00:00Z" },
		] });
		render(<MemoryRouter><Dashboard /></MemoryRouter>);
		expect(screen.getByText("Activity Overview")).toBeInTheDocument();
	});

	it("shows the empty state when no souls exist", () => {
		dashboardMocks.useSouls.mockReturnValue({ souls: [], refetch: vi.fn() });
		dashboardMocks.useStats.mockReturnValue({
			data: {
				souls: { total: 0, healthy: 0, degraded: 0, dead: 0 },
				judgments: { today: 0, failures: 0, avg_latency_ms: 0 },
				alerts: { channels: 0, rules: 0, active_incidents: 0 },
			},
			refetch: vi.fn(),
		});
		dashboardMocks.useClusterStatus.mockReturnValue({
			data: { is_clustered: false, peer_count: 0 },
		});
		dashboardMocks.useJudgments.mockReturnValue({ data: [] });

		render(
			<MemoryRouter>
				<Dashboard />
			</MemoryRouter>,
		);

		expect(screen.getByText("Start Monitoring")).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: /Create First Soul/i }),
		).toHaveAttribute("href", "/souls");
		expect(screen.getByText("No souls yet")).toBeInTheDocument();
	});
});
