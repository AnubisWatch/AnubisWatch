import {
	act,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StatusPages } from "./StatusPages";

const mockCreatePage = vi.fn();
const mockUpdatePage = vi.fn();
const mockDeletePage = vi.fn();
const mockRefetch = vi.fn();
const mockClipboardWriteText = vi.fn();

let mockLoading = false;
let mockError: string | null = null;
let mockSouls = [
	{ id: "soul-1", name: "API", type: "http", enabled: true },
];

let mockPages = [
	{
		id: "page-1",
		name: "Production Status",
		slug: "production",
		description: "Customer-facing services",
		enabled: true,
		theme: "light" as const,
		souls: ["soul-1"],
		subscribers: 7,
	},
];

vi.mock("../api/hooks", async () => {
	const actual = await vi.importActual("../api/hooks");
	return {
		...actual,
		useStatusPages: () => ({
			pages: mockPages,
			loading: mockLoading,
			error: mockError,
			refetch: mockRefetch,
			createPage: mockCreatePage,
			updatePage: mockUpdatePage,
			deletePage: mockDeletePage,
		}),
		useSouls: () => ({ souls: mockSouls }),
	};
});

describe("StatusPages", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockLoading = false;
		mockError = null;
		mockSouls = [{ id: "soul-1", name: "API", type: "http", enabled: true }];
		mockPages = [
			{
				id: "page-1",
				name: "Production Status",
				slug: "production",
				description: "Customer-facing services",
				enabled: true,
				theme: "light" as const,
				souls: ["soul-1"],
				subscribers: 7,
			},
		];
		mockCreatePage.mockResolvedValue(undefined);
		mockUpdatePage.mockResolvedValue(undefined);
		mockDeletePage.mockResolvedValue(undefined);
		mockRefetch.mockResolvedValue(undefined);
		mockClipboardWriteText.mockResolvedValue(undefined);
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText: mockClipboardWriteText },
			configurable: true,
		});
		Object.defineProperty(navigator, "share", {
			value: undefined,
			configurable: true,
		});
		Object.defineProperty(window, "confirm", {
			value: vi.fn(() => true),
			configurable: true,
		});
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("opens the edit modal with page values and saves through updatePage", async () => {
		render(<StatusPages />);

		fireEvent.click(
			screen.getByRole("button", {
				name: /edit status page production status/i,
			}),
		);

		const dialog = screen.getByRole("dialog");
		expect(within(dialog).getByLabelText("Name")).toHaveValue(
			"Production Status",
		);
		expect(within(dialog).getByLabelText("Slug")).toHaveValue("production");
		expect(within(dialog).getByText("Light")).toHaveClass("border-amber-500");

		fireEvent.change(within(dialog).getByLabelText("Name"), {
			target: { value: "Updated Status" },
		});
		fireEvent.click(
			within(dialog).getByRole("button", { name: /save status page/i }),
		);

		await waitFor(() => {
			expect(mockUpdatePage).toHaveBeenCalledWith(
				"page-1",
				expect.objectContaining({
					name: "Updated Status",
					slug: "production",
					enabled: true,
					souls: ["soul-1"],
					subscribers: 7,
				}),
			);
		});
		expect(mockCreatePage).not.toHaveBeenCalled();
	});

	it("creates, filters, refreshes, and deletes status pages", async () => {
		render(<StatusPages />);

		fireEvent.change(screen.getByPlaceholderText("Search status pages..."), {
			target: { value: "prod" },
		});
		expect(screen.getByText("Production Status")).toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("All Pages"), {
			target: { value: "disabled" },
		});
		expect(screen.queryByText("Production Status")).not.toBeInTheDocument();

		fireEvent.change(screen.getByDisplayValue("Disabled Only"), {
			target: { value: "all" },
		});
		fireEvent.click(
			screen.getByRole("button", { name: /refresh status pages/i }),
		);
		await waitFor(() => expect(mockRefetch).toHaveBeenCalled());

		fireEvent.click(screen.getByRole("button", { name: /create page/i }));
		const dialog = screen.getByRole("dialog");
		fireEvent.change(within(dialog).getByLabelText("Name"), {
			target: { value: "Public Status" },
		});
		fireEvent.change(within(dialog).getByLabelText("Slug"), {
			target: { value: "public" },
		});
		fireEvent.click(within(dialog).getByRole("button", { name: "Dark" }));
		fireEvent.click(within(dialog).getByText("API"));
		fireEvent.click(
			within(dialog).getByRole("button", { name: /create status page/i }),
		);

		await waitFor(() => {
			expect(mockCreatePage).toHaveBeenCalledWith(
				expect.objectContaining({
					name: "Public Status",
					slug: "public",
					theme: "dark",
					souls: ["soul-1"],
				}),
			);
		});

		fireEvent.click(
			screen.getByRole("button", {
				name: /delete status page production status/i,
			}),
		);
		await waitFor(() => expect(mockDeletePage).toHaveBeenCalledWith("page-1"));
	});

	it("falls back to copying the page URL when native share is unavailable", async () => {
		render(<StatusPages />);

		fireEvent.click(
			screen.getByRole("button", {
				name: /share status page production status/i,
			}),
		);

		await waitFor(() => {
			expect(mockClipboardWriteText).toHaveBeenCalledWith(
				expect.stringMatching(/\/status\/production$/),
			);
		});
	});

	it("renders loading and error states and retries", async () => {
		mockLoading = true;
		const { rerender } = render(<StatusPages />);
		expect(screen.getByRole("status", { name: "Loading status pages" })).toBeInTheDocument();
		mockLoading = false;
		mockError = "status unavailable";
		rerender(<StatusPages />);
		fireEvent.click(screen.getByRole("button", { name: "Try Again" }));
		await waitFor(() => expect(mockRefetch).toHaveBeenCalledOnce());
	});

	it("validates, resets, escapes, and handles empty services", async () => {
		mockPages = [];
		mockSouls = [];
		render(<StatusPages />);
		expect(screen.getByText("No status pages yet")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "Create Your First Page" }));
		const dialog = screen.getByRole("dialog");
		expect(within(dialog).getByText("No souls configured. Add services first.")).toBeInTheDocument();
		expect(within(dialog).getByRole("button", { name: "Create Status Page" })).toBeDisabled();
		fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "API Status" } });
		expect(within(dialog).getByLabelText("Slug")).toHaveValue("api-status");
		fireEvent.keyDown(dialog, { key: "Escape" });
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});

	it("cancels deletion and closes the modal with both controls", () => {
		Object.defineProperty(window, "confirm", { value: vi.fn(() => false), configurable: true });
		render(<StatusPages />);
		fireEvent.click(screen.getByLabelText(/delete status page production status/i));
		expect(mockDeletePage).not.toHaveBeenCalled();
		fireEvent.click(screen.getByRole("button", { name: /create page/i }));
		fireEvent.click(screen.getByLabelText("Close dialog"));
		fireEvent.click(screen.getByRole("button", { name: /create page/i }));
		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});

	it("uses native sharing, tolerates abort, and falls back after other failures", async () => {
		const share = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new DOMException("cancel", "AbortError")).mockRejectedValueOnce(new Error("no share"));
		Object.defineProperty(navigator, "share", { value: share, configurable: true });
		render(<StatusPages />);
		const button = screen.getByLabelText(/share status page production status/i);
		fireEvent.click(button);
		await waitFor(() => expect(share).toHaveBeenCalledTimes(1));
		fireEvent.click(button);
		await waitFor(() => expect(share).toHaveBeenCalledTimes(2));
		expect(mockClipboardWriteText).not.toHaveBeenCalled();
		fireEvent.click(button);
		await waitFor(() => expect(mockClipboardWriteText).toHaveBeenCalled());
	});

	it("keeps the editor open when saving fails and can deselect services", async () => {
		mockCreatePage.mockRejectedValueOnce(new Error("failed"));
		render(<StatusPages />);
		fireEvent.click(screen.getByRole("button", { name: /create page/i }));
		const dialog = screen.getByRole("dialog");
		fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Public" } });
		fireEvent.click(within(dialog).getByText("API"));
		fireEvent.click(within(dialog).getByText("API"));
		fireEvent.click(within(dialog).getByRole("button", { name: "Create Status Page" }));
		await waitFor(() => expect(mockCreatePage).toHaveBeenCalled());
		expect(screen.getByRole("dialog")).toBeInTheDocument();
	});

	it("covers theme variants, domain precedence, copy feedback, and description search", async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		mockPages = [
			{ id: "none", name: "No Theme", slug: "none", description: "Needle text", enabled: false, souls: [], subscribers: 0 },
			{ id: "string", name: "String Theme", slug: "string", description: "", enabled: true, theme: "auto" as const, souls: [], subscribers: 0 },
			{ id: "light", name: "Light Object", slug: "light", enabled: true, theme: { background_color: "#ffffff" }, domain: "primary.example.com", custom_domain: "ignored.example.com", souls: [], subscribers: 0 },
			{ id: "custom", name: "Custom Object", slug: "custom", enabled: true, theme: { background_color: "#123456" }, souls: ["missing"], subscribers: 0 },
		];
		render(<StatusPages />);
		for (const label of ["Dark", "Auto", "Light", "Custom"]) expect(screen.getByText(label)).toBeInTheDocument();
		expect(screen.getAllByRole("link", { name: "View" })[2]).toHaveAttribute("href", "https://primary.example.com");
		fireEvent.change(screen.getByPlaceholderText("Search status pages..."), { target: { value: "needle" } });
		expect(screen.getByText("No Theme")).toBeInTheDocument();
		fireEvent.change(screen.getByPlaceholderText("Search status pages..."), { target: { value: "" } });
		fireEvent.click(screen.getAllByRole("button", { name: "Copy URL" })[0]);
		await waitFor(() => expect(screen.getByRole("button", { name: "Copied!" })).toBeInTheDocument());
		await act(async () => {
			await vi.advanceTimersByTimeAsync(2000);
		});
		await waitFor(() => expect(screen.queryByRole("button", { name: "Copied!" })).not.toBeInTheDocument());
	});

	it("sanitizes an edited slug and changes theme and description", async () => {
		render(<StatusPages />);
		fireEvent.click(screen.getByLabelText(/edit status page production status/i));
		const dialog = screen.getByRole("dialog");
		fireEvent.change(within(dialog).getByLabelText("Slug"), { target: { value: "UP DATE!" } });
		fireEvent.change(within(dialog).getByLabelText("Description"), { target: { value: "Changed" } });
		fireEvent.click(within(dialog).getByRole("button", { name: "Auto" }));
		fireEvent.click(within(dialog).getByRole("button", { name: "Save Status Page" }));
		await waitFor(() => expect(mockUpdatePage).toHaveBeenCalledWith("page-1", expect.objectContaining({ slug: "update", description: "Changed", theme: "auto" })));
	});

	it("uses custom_domain for the domain count and external view link", () => {
		mockPages = [
			{
				id: "page-custom-domain",
				name: "Public Status",
				slug: "public",
				description: "Custom domain page",
				enabled: true,
				theme: "dark" as const,
				custom_domain: "status.example.com",
				souls: [],
				subscribers: 0,
			},
		];

		render(<StatusPages />);

		expect(
			screen.getByText("Custom Domains").nextElementSibling,
		).toHaveTextContent("1");
		expect(screen.getByRole("link", { name: /view/i })).toHaveAttribute(
			"href",
			"https://status.example.com",
		);
	});
});
