import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Judgments } from "./Judgments";

const judgmentsMocks = vi.hoisted(() => ({
	useJudgments: vi.fn(),
}));

vi.mock("../api/hooks", async () => {
	const actual = await vi.importActual("../api/hooks");
	return {
		...actual,
		useJudgments: judgmentsMocks.useJudgments,
	};
});

const createObjectURL = vi.fn(() => "blob:test-url");
const revokeObjectURL = vi.fn();
const clickMock = vi.fn();

describe("Judgments", () => {
	const sampleJudgments = [
		{
			id: "j-1",
			soul_id: "soul-1",
			soul_name: "Payments API",
			status: "passed" as const,
			latency: 120,
			status_code: 200,
			region: "eu-west",
			timestamp: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
			error: "",
		},
		{
			id: "j-2",
			soul_id: "soul-2",
			soul_name: "Billing Worker",
			status: "failed" as const,
			latency: 1800,
			status_code: 0,
			region: "us-east",
			timestamp: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
			error: "Connection timeout",
		},
	];

	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(URL, "createObjectURL", {
			value: createObjectURL,
			configurable: true,
		});
		Object.defineProperty(URL, "revokeObjectURL", {
			value: revokeObjectURL,
			configurable: true,
		});
		Object.defineProperty(HTMLAnchorElement.prototype, "click", {
			value: clickMock,
			configurable: true,
		});
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("renders loading and error states", () => {
		judgmentsMocks.useJudgments.mockReturnValue({
			data: [],
			loading: true,
			error: null,
			refetch: vi.fn(),
		});
		const { rerender } = render(
			<MemoryRouter>
				<Judgments />
			</MemoryRouter>,
		);
		expect(document.querySelector(".animate-spin")).toBeInTheDocument();

		judgmentsMocks.useJudgments.mockReturnValue({
			data: [],
			loading: false,
			error: "offline",
			refetch: vi.fn(),
		});
		rerender(
			<MemoryRouter>
				<Judgments />
			</MemoryRouter>,
		);
		expect(screen.getByText("offline")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /try again/i }),
		).toBeInTheDocument();
	});

	it("filters, refreshes, and exports judgments", async () => {
		const refetch = vi.fn().mockResolvedValue(undefined);
		judgmentsMocks.useJudgments.mockReturnValue({
			data: sampleJudgments,
			loading: false,
			error: null,
			refetch,
		});

		render(
			<MemoryRouter>
				<Judgments />
			</MemoryRouter>,
		);

		expect(
			screen.getByRole("link", { name: /payments api/i }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: /billing worker/i }),
		).toBeInTheDocument();
		expect(screen.getByText("Connection timeout")).toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("All Judgments"), {
			target: { value: "failed" },
		});
		expect(screen.queryByText("Payments API")).not.toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: /billing worker/i }),
		).toBeInTheDocument();

		fireEvent.change(
			screen.getByPlaceholderText("Search by soul name or region..."),
			{
				target: { value: "eu-west" },
			},
		);
		fireEvent.change(screen.getByDisplayValue("Failed Only"), {
			target: { value: "all" },
		});
		expect(
			screen.getByRole("link", { name: /payments api/i }),
		).toBeInTheDocument();
		expect(screen.queryByText("Billing Worker")).not.toBeInTheDocument();

		fireEvent.click(screen.getByRole("button", { name: /refresh judgments/i }));
		await waitFor(() => expect(refetch).toHaveBeenCalled());

		fireEvent.click(screen.getByRole("button", { name: /export/i }));
		expect(createObjectURL).toHaveBeenCalled();
		expect(clickMock).toHaveBeenCalled();
		expect(revokeObjectURL).toHaveBeenCalledWith("blob:test-url");
	});

	it("covers every time range, status-code band, CSV escaping, and refresh completion", async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		vi.setSystemTime(new Date("2026-07-06T12:00:00Z"));
		const refetch = vi.fn().mockResolvedValue(undefined);
		judgmentsMocks.useJudgments.mockReturnValue({
			data: [
				{ ...sampleJudgments[0], id: "high", timestamp: "2026-07-06T11:30:00Z", soul_name: 'Quoted "Name"', status_code: 200 },
				{ ...sampleJudgments[0], id: "mid", timestamp: "2026-07-05T12:00:00Z", soul_name: "", region: undefined, error: undefined, status_code: 404 },
				{ ...sampleJudgments[1], id: "low", timestamp: "2026-06-10T12:00:00Z", status_code: 503 },
			], loading: false, error: null, refetch,
		});
		render(<MemoryRouter><Judgments /></MemoryRouter>);
		const range = screen.getByDisplayValue("Last 24 Hours");
		fireEvent.change(range, { target: { value: "1h" } });
		expect(screen.getByText('Quoted "Name"')).toBeInTheDocument();
		fireEvent.change(range, { target: { value: "7d" } });
		expect(screen.getByText("soul-1")).toBeInTheDocument();
		fireEvent.change(range, { target: { value: "30d" } });
		expect(screen.getByText("Billing Worker")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /export/i }));
		expect(createObjectURL).toHaveBeenCalled();
		fireEvent.click(screen.getByLabelText("Refresh judgments"));
		await waitFor(() => expect(refetch).toHaveBeenCalled());
		await act(async () => { await vi.advanceTimersByTimeAsync(500); });
		expect(screen.getByLabelText("Refresh judgments")).not.toHaveClass("animate-spin");
	});

	it("shows latency and status code from the API, not a purity score", async () => {
		judgmentsMocks.useJudgments.mockReturnValue({
			data: [
				{ ...sampleJudgments[0], id: "a", latency: 100, status_code: 200 },
				{ ...sampleJudgments[0], id: "b", latency: 300, status_code: 0 },
				{ ...sampleJudgments[1], id: "c", latency: 2000, status_code: 503 },
			],
			loading: false,
			error: null,
			refetch: vi.fn(),
		});
		render(<MemoryRouter><Judgments /></MemoryRouter>);

		const headers = screen.getAllByRole("columnheader").map((h) => h.textContent);
		expect(headers).toEqual(["Status", "Soul", "Latency", "Code", "Region", "Time"]);
		expect(screen.queryByText(/purity/i)).not.toBeInTheDocument();
		const rows = screen.getAllByRole("row").slice(1);
		expect(rows.map((r) => r.querySelectorAll("td")[2].textContent)).toEqual(["100ms", "300ms", "2000ms"]);
		expect(rows.map((r) => r.querySelectorAll("td")[3].textContent)).toEqual(["200", "—", "503"]);
		expect(screen.getByText("503")).toHaveClass("text-rose-400");
		// Average of 100, 300, 2000 and nearest-rank p95.
		expect(screen.getByText("Avg Latency").nextElementSibling).toHaveTextContent("800ms");
		expect(screen.getByText("P95 Latency").nextElementSibling).toHaveTextContent("2000ms");

		let blob: Blob | undefined;
		createObjectURL.mockImplementationOnce(((b: Blob) => { blob = b; return "blob:test-url"; }) as never);
		fireEvent.click(screen.getByRole("button", { name: /export/i }));
		const csv = await blob!.text();
		const [header, ...lines] = csv.split("\n");
		expect(header).toBe('"id","soul_id","soul_name","status","latency_ms","status_code","region","timestamp","error"');
		expect(lines[0]).toContain('"a","soul-1","Payments API","passed","100","200"');
		expect(lines[1]).toContain('"b","soul-1","Payments API","passed","300","0"');
		expect(csv).not.toMatch(/purity/);
	});

	it("retries from the error state", async () => {
		const refetch = vi.fn().mockResolvedValue(undefined);
		judgmentsMocks.useJudgments.mockReturnValue({ data: [], loading: false, error: "offline", refetch });
		render(<MemoryRouter><Judgments /></MemoryRouter>);
		fireEvent.click(screen.getByRole("button", { name: "Try Again" }));
		await waitFor(() => expect(refetch).toHaveBeenCalledOnce());
	});

	it("excludes judgments with invalid timestamps", () => {
		judgmentsMocks.useJudgments.mockReturnValue({
			data: [
				...sampleJudgments,
				{
					...sampleJudgments[0],
					id: "invalid-time",
					soul_name: "Invalid Clock",
					timestamp: "not-a-date",
				},
			],
			loading: false,
			error: null,
			refetch: vi.fn(),
		});

		render(
			<MemoryRouter>
				<Judgments />
			</MemoryRouter>,
		);

		expect(screen.queryByText("Invalid Clock")).not.toBeInTheDocument();
		expect(screen.queryByText("Invalid Date")).not.toBeInTheDocument();
	});

	it("shows empty state when no judgments exist", () => {
		judgmentsMocks.useJudgments.mockReturnValue({
			data: [],
			loading: false,
			error: null,
			refetch: vi.fn(),
		});

		render(
			<MemoryRouter>
				<Judgments />
			</MemoryRouter>,
		);

		expect(screen.getByText("No judgments yet")).toBeInTheDocument();
		expect(
			screen.getByRole("link", { name: /create your first soul/i }),
		).toHaveAttribute("href", "/souls");
	});
});
