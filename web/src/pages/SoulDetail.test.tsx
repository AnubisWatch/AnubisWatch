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
		await waitFor(() => {
			expect(mockDeleteSoul).toHaveBeenCalled();
			expect(mockNavigate).toHaveBeenCalledWith("/souls");
		});
	});

	it("surfaces force-check failures and respects delete cancellation", async () => {
		(
			globalThis.confirm as unknown as ReturnType<typeof vi.fn>
		).mockReturnValueOnce(false);
		mockForceCheck.mockRejectedValueOnce(new Error("network down"));
		renderDetail();

		fireEvent.click(screen.getByRole("button", { name: /test now/i }));
		await waitFor(() => {
			expect(
				screen.getByText(/Check failed: network down/i),
			).toBeInTheDocument();
		});

		fireEvent.click(screen.getByLabelText("Delete soul"));
		expect(mockDeleteSoul).not.toHaveBeenCalled();
	});
});
