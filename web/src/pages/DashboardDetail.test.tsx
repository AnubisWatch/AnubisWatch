import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DashboardDetail } from "./DashboardDetail";

const mockGet = vi.fn();
const mockPost = vi.fn();
const mockPut = vi.fn();
const mockNavigate = vi.fn();

vi.mock("react-router-dom", async () => {
	const actual = await vi.importActual("react-router-dom");
	return {
		...actual,
		useNavigate: () => mockNavigate,
	};
});

vi.mock("../api/client", () => ({
	api: {
		get: (...args: unknown[]) => mockGet(...(args as [string])),
		post: (...args: unknown[]) => mockPost(...(args as [string, unknown])),
		put: (...args: unknown[]) => mockPut(...(args as [string, unknown])),
	},
}));

vi.mock("../components/widgets/StatWidget", () => ({
	StatWidget: () => <div>Stat Widget</div>,
}));
vi.mock("../components/widgets/LineChartWidget", () => ({
	LineChartWidget: () => <div>Line Widget</div>,
}));
vi.mock("../components/widgets/BarChartWidget", () => ({
	BarChartWidget: () => <div>Bar Widget</div>,
}));
vi.mock("../components/widgets/GaugeWidget", () => ({
	GaugeWidget: () => <div>Gauge Widget</div>,
}));
vi.mock("../components/widgets/TableWidget", () => ({
	TableWidget: () => <div>Table Widget</div>,
}));

const dashboard = {
	id: "dash-1",
	name: "Operations",
	description: "Main operations dashboard",
	refresh_sec: 0,
	widgets: [
		{
			id: "widget-1",
			title: "Latency",
			type: "line_chart" as const,
			grid: { x: 0, y: 0, width: 4, height: 2 },
			query: { source: "judgments", metric: "latency", time_range: "24h" },
		},
	],
};

function renderAt(path: string) {
	render(
		<MemoryRouter initialEntries={[path]}>
			<Routes>
				<Route path="/dashboards/new" element={<DashboardDetail />} />
				<Route path="/dashboards/:id" element={<DashboardDetail />} />
			</Routes>
		</MemoryRouter>,
	);
}

describe("DashboardDetail", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockGet.mockResolvedValue(dashboard);
		mockPost.mockResolvedValue({ id: "new-dashboard" });
		mockPut.mockResolvedValue(undefined);
	});

	it("creates a new dashboard and navigates to it", async () => {
		renderAt("/dashboards/new");

		fireEvent.change(screen.getByLabelText("Name"), {
			target: { value: "Exec Overview" },
		});
		fireEvent.change(screen.getByLabelText("Description"), {
			target: { value: "KPIs" },
		});
		fireEvent.change(screen.getByLabelText("Refresh Interval"), {
			target: { value: "300" },
		});
		fireEvent.click(screen.getByRole("button", { name: /create dashboard/i }));

		await waitFor(() => {
			expect(mockPost).toHaveBeenCalledWith("/dashboards", {
				name: "Exec Overview",
				description: "KPIs",
				widgets: [],
				refresh_sec: 300,
			});
		});
		expect(mockNavigate).toHaveBeenCalledWith("/dashboards/new-dashboard");
	});

	it("shows not found state when dashboard fetch fails", async () => {
		mockGet.mockRejectedValueOnce(new Error("not found"));
		renderAt("/dashboards/dash-404");

		await waitFor(() => {
			expect(screen.getByText("Dashboard Not Found")).toBeInTheDocument();
		});
	});

	it("adds and deletes widgets while editing an existing dashboard", async () => {
		renderAt("/dashboards/dash-1");

		await screen.findByText("Operations");
		fireEvent.click(screen.getByRole("button", { name: /edit/i }));
		fireEvent.click(screen.getByRole("button", { name: /add widget/i }));

		fireEvent.change(screen.getByPlaceholderText("Widget title"), {
			target: { value: "Open Incidents" },
		});
		fireEvent.change(screen.getByDisplayValue("Stat"), {
			target: { value: "table" },
		});
		fireEvent.change(screen.getByDisplayValue("Souls"), {
			target: { value: "alerts" },
		});
		fireEvent.change(screen.getByPlaceholderText("metric"), {
			target: { value: "open" },
		});
		fireEvent.click(screen.getAllByRole("button", { name: /add widget/i })[1]);

		await waitFor(() => {
			expect(mockPut).toHaveBeenCalledWith(
				"/dashboards/dash-1",
				expect.objectContaining({
					widgets: expect.arrayContaining([
						expect.objectContaining({
							title: "Open Incidents",
							type: "table",
							query: { source: "alerts", metric: "open", time_range: "24h" },
						}),
					]),
				}),
			);
		});

		fireEvent.click(screen.getByLabelText("Delete widget"));
		await waitFor(() => {
			expect(mockPut).toHaveBeenCalledWith(
				"/dashboards/dash-1",
				expect.objectContaining({
					widgets: [],
				}),
			);
		});
	});
});
