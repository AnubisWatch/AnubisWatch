import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Incidents } from "./Incidents";

const mockGet = vi.fn();
const mockPost = vi.fn();

vi.mock("../api/client", () => ({
	api: {
		get: (...args: unknown[]) => mockGet(...(args as [string])),
		post: (...args: unknown[]) => mockPost(...(args as [string])),
	},
}));

const incidents = [
	{
		id: "inc-1",
		rule_id: "rule-1",
		soul_id: "soul-1",
		soul_name: "Payments API",
		workspace_id: "default",
		status: "open" as const,
		severity: "critical" as const,
		started_at: "2026-07-06T08:00:00Z",
		escalation_level: 2,
	},
	{
		id: "inc-2",
		rule_id: "rule-2",
		soul_id: "soul-2",
		soul_name: "Billing Worker",
		workspace_id: "default",
		status: "acknowledged" as const,
		severity: "warning" as const,
		started_at: "2026-07-06T07:30:00Z",
		acked_at: "2026-07-06T07:45:00Z",
		acked_by: "anubis-admin",
		escalation_level: 0,
	},
	{
		id: "inc-3",
		rule_id: "rule-3",
		soul_id: "soul-3",
		workspace_id: "default",
		status: "resolved" as const,
		severity: "info" as const,
		started_at: "2026-07-05T07:30:00Z",
		resolved_at: "2026-07-05T08:00:00Z",
		resolved_by: "operator",
		escalation_level: 0,
	},
];

describe("Incidents", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockGet.mockResolvedValue(incidents);
		mockPost.mockResolvedValue(undefined);
	});

	it("renders incidents, filters them, and shows action buttons by status", async () => {
		render(<Incidents />);

		await waitFor(() => {
			expect(screen.getByText("Payments API")).toBeInTheDocument();
			expect(screen.getByText("Billing Worker")).toBeInTheDocument();
			expect(screen.getByText("soul-3")).toBeInTheDocument();
		});

		expect(
			screen.getByText("Open", { selector: "p.text-gray-400.text-sm" })
				.nextElementSibling,
		).toHaveTextContent("1");
		expect(
			screen.getByText("Acknowledged", {
				selector: "p.text-gray-400.text-sm",
			}).nextElementSibling,
		).toHaveTextContent("1");
		expect(
			screen.getByText("Resolved", { selector: "p.text-gray-400.text-sm" })
				.nextElementSibling,
		).toHaveTextContent("1");
		expect(
			screen.getByText("Critical", { selector: "p.text-gray-400.text-sm" })
				.nextElementSibling,
		).toHaveTextContent("1");
		expect(
			screen.getByRole("button", { name: "Acknowledge" }),
		).toBeInTheDocument();
		expect(screen.getAllByRole("button", { name: "Resolve" })).toHaveLength(2);

		fireEvent.change(screen.getByDisplayValue("All Status"), {
			target: { value: "acknowledged" },
		});
		expect(screen.queryByText("Payments API")).not.toBeInTheDocument();
		expect(screen.getByText("Billing Worker")).toBeInTheDocument();

		fireEvent.change(screen.getByPlaceholderText("Search incidents..."), {
			target: { value: "payments" },
		});
		expect(screen.getByText("No incidents found")).toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("Acknowledged"), {
			target: { value: "all" },
		});
		fireEvent.change(screen.getByDisplayValue("All Severity"), {
			target: { value: "critical" },
		});
		fireEvent.change(screen.getByPlaceholderText("Search incidents..."), {
			target: { value: "payments" },
		});

		expect(screen.getByText("Payments API")).toBeInTheDocument();
		expect(screen.queryByText("Billing Worker")).not.toBeInTheDocument();
		expect(screen.getByText("Level 2")).toBeInTheDocument();
	});

	it("acknowledges and resolves incidents, then refetches data", async () => {
		render(<Incidents />);

		await screen.findByText("Payments API");

		fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
		await waitFor(() => {
			expect(mockPost).toHaveBeenCalledWith("/incidents/inc-1/acknowledge");
		});

		fireEvent.click(screen.getAllByRole("button", { name: "Resolve" })[0]);
		await waitFor(() => {
			expect(mockPost).toHaveBeenCalledWith("/incidents/inc-1/resolve");
		});

		expect(mockGet).toHaveBeenCalledTimes(3);
	});

	it("shows fetch and action errors", async () => {
		mockGet.mockRejectedValueOnce(new Error("incidents unavailable"));
		render(<Incidents />);

		await waitFor(() => {
			expect(screen.getByText("incidents unavailable")).toBeInTheDocument();
		});

		mockGet.mockResolvedValue(incidents);
		mockPost.mockRejectedValueOnce(new Error("resolve failed"));
		render(<Incidents />);

		await screen.findByText("Payments API");
		fireEvent.click(screen.getAllByRole("button", { name: "Resolve" })[0]);

		await waitFor(() => {
			expect(screen.getByText("resolve failed")).toBeInTheDocument();
		});
	});
});
