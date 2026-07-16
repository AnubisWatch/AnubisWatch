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
}));

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
		mockUseSoulJudgments.mockReturnValueOnce({ data: undefined, loading: true, error: null, refetch: mockRefetchJudgments });
		renderDetail();
		expect(document.querySelectorAll(".animate-spin").length).toBeGreaterThan(0);

		mockUseSoulJudgments.mockReturnValueOnce({ data: [], loading: false, error: "history unavailable", refetch: mockRefetchJudgments });
		renderDetail();
		expect(screen.getByText("history unavailable")).toBeInTheDocument();

		mockUseSoulJudgments.mockReturnValueOnce({ data: [], loading: false, error: null, refetch: mockRefetchJudgments });
		renderDetail();
		expect(screen.getByText("No judgments yet")).toBeInTheDocument();
		expect(screen.getAllByText("No checks yet")).toHaveLength(3);
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
		mockUseSoulJudgments.mockReturnValue({ data: [{ id: "j-fast", status: "passed", latency: 50, timestamp: new Date().toISOString(), purity: 100 }], loading: false, error: null, refetch: mockRefetchJudgments });
		renderDetail();
		expect(screen.getByText("Fast")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("tab", { name: /settings/i }));
		expect(screen.getByText("45s")).toBeInTheDocument();
		expect(screen.getByText("global")).toBeInTheDocument();
	});
});
