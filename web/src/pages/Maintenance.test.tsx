import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Maintenance } from "./Maintenance";

const mocks = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	put: vi.fn(),
	delete: vi.fn(),
}));

vi.mock("../api/client", () => ({
	api: {
		get: mocks.get,
		post: mocks.post,
		put: mocks.put,
		delete: mocks.delete,
	},
}));

describe("Maintenance", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(window, "confirm", {
			value: vi.fn(() => true),
			configurable: true,
		});
		mocks.get.mockImplementation((endpoint: string) => {
			if (endpoint === "/souls") {
				return Promise.resolve({
					data: [
						{ id: "soul-1", name: "API", type: "http", enabled: true },
						{ id: "soul-2", name: "Database", type: "tcp", enabled: true },
					],
				});
			}
			return Promise.resolve([]);
		});
		mocks.post.mockResolvedValue({});
		mocks.put.mockResolvedValue({});
		mocks.delete.mockResolvedValue({});
	});

	it("creates a maintenance window with ISO timestamps and cleaned tags", async () => {
		render(<Maintenance />);

		await screen.findByText("No maintenance windows");
		fireEvent.click(screen.getByRole("button", { name: /add window/i }));

		const dialog = screen.getByRole("dialog");
		fireEvent.change(
			within(dialog).getByPlaceholderText(/database migration/i),
			{
				target: { value: "Database Migration" },
			},
		);
		fireEvent.change(within(dialog).getByPlaceholderText(/what maintenance/i), {
			target: { value: "Primary DB upgrade" },
		});

		const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]');
		fireEvent.change(timeInputs[0], { target: { value: "2030-01-01T10:00" } });
		fireEvent.change(timeInputs[1], { target: { value: "2030-01-01T12:00" } });
		fireEvent.change(within(dialog).getByRole("combobox"), {
			target: { value: "weekly" },
		});
		fireEvent.change(
			within(dialog).getByPlaceholderText(/database, production/i),
			{
				target: { value: " database, production, " },
			},
		);
		fireEvent.click(within(dialog).getByRole("button", { name: /apihttp/i }));

		fireEvent.click(
			within(dialog).getByRole("button", { name: /create window/i }),
		);

		await waitFor(() => {
			expect(mocks.post).toHaveBeenCalledWith("/maintenance", {
				name: "Database Migration",
				description: "Primary DB upgrade",
				start_time: new Date("2030-01-01T10:00").toISOString(),
				end_time: new Date("2030-01-01T12:00").toISOString(),
				recurring: "weekly",
				enabled: true,
				tags: ["database", "production"],
				soul_ids: ["soul-1"],
			});
		});
	});

	it("requires an explicit service, tag, or all-services scope", async () => {
		render(<Maintenance />);

		await screen.findByText("No maintenance windows");
		fireEvent.click(screen.getByRole("button", { name: /add window/i }));

		const dialog = screen.getByRole("dialog");
		fireEvent.change(
			within(dialog).getByPlaceholderText(/database migration/i),
			{
				target: { value: "Unscoped Window" },
			},
		);
		const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]');
		fireEvent.change(timeInputs[0], { target: { value: "2030-01-01T10:00" } });
		fireEvent.change(timeInputs[1], { target: { value: "2030-01-01T12:00" } });

		fireEvent.click(
			within(dialog).getByRole("button", { name: /create window/i }),
		);

		expect(
			await within(dialog).findByText(
				"Select at least one service or tag, or choose all services",
			),
		).toBeInTheDocument();
		expect(mocks.post).not.toHaveBeenCalled();
	});

	it("allows workspace-wide maintenance only when all services is selected", async () => {
		render(<Maintenance />);

		await screen.findByText("No maintenance windows");
		fireEvent.click(screen.getByRole("button", { name: /add window/i }));

		const dialog = screen.getByRole("dialog");
		fireEvent.change(
			within(dialog).getByPlaceholderText(/database migration/i),
			{
				target: { value: "Global Window" },
			},
		);
		const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]');
		fireEvent.change(timeInputs[0], { target: { value: "2030-01-01T10:00" } });
		fireEvent.change(timeInputs[1], { target: { value: "2030-01-01T12:00" } });
		fireEvent.click(within(dialog).getByLabelText("All services"));

		fireEvent.click(
			within(dialog).getByRole("button", { name: /create window/i }),
		);

		await waitFor(() => {
			expect(mocks.post).toHaveBeenCalledWith(
				"/maintenance",
				expect.objectContaining({
					name: "Global Window",
					tags: [],
					soul_ids: [],
				}),
			);
		});
	});

	it("blocks saving when the end time is not after the start time", async () => {
		render(<Maintenance />);

		await screen.findByText("No maintenance windows");
		fireEvent.click(screen.getByRole("button", { name: /add window/i }));

		const dialog = screen.getByRole("dialog");
		fireEvent.change(
			within(dialog).getByPlaceholderText(/database migration/i),
			{
				target: { value: "Bad Window" },
			},
		);
		const timeInputs = dialog.querySelectorAll('input[type="datetime-local"]');
		fireEvent.change(timeInputs[0], { target: { value: "2030-01-01T12:00" } });
		fireEvent.change(timeInputs[1], { target: { value: "2030-01-01T10:00" } });

		fireEvent.click(
			within(dialog).getByRole("button", { name: /create window/i }),
		);

		expect(
			await within(dialog).findByText("End time must be after start time"),
		).toBeInTheDocument();
		expect(mocks.post).not.toHaveBeenCalled();
	});

	it("renders existing windows, filters them, toggles enabled state, edits, deletes, and shows API errors", async () => {
		mocks.get.mockImplementation((endpoint: string) => {
			if (endpoint === "/souls") {
				return Promise.resolve({
					data: [{ id: "soul-1", name: "API", type: "http", enabled: true }],
				});
			}
			if (endpoint === "/maintenance") {
				return Promise.resolve([
					{
						id: "mw-1",
						name: "Active Window",
						description: "Current maintenance",
						soul_ids: ["soul-1"],
						tags: ["prod"],
						start_time: "2030-01-01T10:00:00.000Z",
						end_time: "2030-01-01T12:00:00.000Z",
						recurring: "daily",
						enabled: true,
					},
					{
						id: "mw-2",
						name: "Disabled Window",
						description: "Paused",
						soul_ids: [],
						tags: [],
						start_time: "2030-01-02T10:00:00.000Z",
						end_time: "2030-01-02T12:00:00.000Z",
						recurring: "",
						enabled: false,
					},
				]);
			}
			return Promise.resolve([]);
		});

		vi.setSystemTime(new Date("2030-01-01T11:00:00.000Z"));
		render(<Maintenance />);

		await screen.findByText("Active Window");
		expect(screen.getByText("Disabled Window")).toBeInTheDocument();

		fireEvent.change(
			screen.getByPlaceholderText("Search maintenance windows..."),
			{ target: { value: "active" } },
		);
		expect(screen.getByText("Active Window")).toBeInTheDocument();
		expect(screen.queryByText("Disabled Window")).not.toBeInTheDocument();

		fireEvent.change(
			screen.getByPlaceholderText("Search maintenance windows..."),
			{
				target: { value: "" },
			},
		);
		fireEvent.change(screen.getByDisplayValue("All Windows"), {
			target: { value: "disabled" },
		});
		expect(
			screen.getByLabelText(/edit maintenance window disabled window/i),
		).toBeInTheDocument();
		expect(screen.queryByText("Active Window")).not.toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("Disabled"), {
			target: { value: "all" },
		});
		fireEvent.change(
			screen.getByPlaceholderText("Search maintenance windows..."),
			{ target: { value: "" } },
		);

		fireEvent.click(
			screen.getByLabelText(/disable maintenance window active window/i),
		);
		await waitFor(() =>
			expect(mocks.put).toHaveBeenCalledWith("/maintenance/mw-1", {
				enabled: false,
			}),
		);

		fireEvent.click(
			screen.getByLabelText(/edit maintenance window active window/i),
		);
		const dialog = screen.getByRole("dialog");
		fireEvent.change(within(dialog).getByDisplayValue("Active Window"), {
			target: { value: "Edited Window" },
		});
		fireEvent.click(
			within(dialog).getByRole("button", { name: /save changes/i }),
		);
		await waitFor(() => {
			expect(mocks.put).toHaveBeenCalledWith(
				"/maintenance/mw-1",
				expect.objectContaining({ name: "Edited Window" }),
			);
		});

		fireEvent.click(
			screen.getByLabelText(/delete maintenance window active window/i),
		);
		await waitFor(() =>
			expect(mocks.delete).toHaveBeenCalledWith("/maintenance/mw-1"),
		);

		mocks.post.mockRejectedValueOnce(new Error("save failed"));
		fireEvent.click(screen.getByRole("button", { name: /add window/i }));
		const errorDialog = screen.getByRole("dialog");
		fireEvent.change(
			within(errorDialog).getByPlaceholderText(/database migration/i),
			{ target: { value: "Failure Window" } },
		);
		const errorTimeInputs = errorDialog.querySelectorAll(
			'input[type="datetime-local"]',
		);
		fireEvent.change(errorTimeInputs[0], {
			target: { value: "2030-01-01T10:00" },
		});
		fireEvent.change(errorTimeInputs[1], {
			target: { value: "2030-01-01T12:00" },
		});
		fireEvent.click(within(errorDialog).getByLabelText("All services"));
		fireEvent.click(
			within(errorDialog).getByRole("button", { name: /create window/i }),
		);
		expect(
			await within(errorDialog).findByText("save failed"),
		).toBeInTheDocument();
	});
});
