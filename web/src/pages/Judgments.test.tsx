import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
			purity: 96,
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
			purity: 42,
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
