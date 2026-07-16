import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Alerts } from "./Alerts";

const mockCreateChannel = vi.fn();
const mockUpdateChannel = vi.fn();
const mockDeleteChannel = vi.fn();
const mockTestChannel = vi.fn();
const mockCreateRule = vi.fn();
const mockUpdateRule = vi.fn();
const mockDeleteRule = vi.fn();
const mockAcknowledgeIncident = vi.fn();
const mockRefetchChannels = vi.fn();
const mockRefetchRules = vi.fn();
const mockRefetchIncidents = vi.fn();
const mockUseChannels = vi.fn();
const mockUseRules = vi.fn();
const mockUseIncidents = vi.fn();

vi.mock("../api/hooks", async () => {
	const actual = await vi.importActual("../api/hooks");
	return {
		...actual,
		useChannels: () => mockUseChannels(),
		useRules: () => mockUseRules(),
		useIncidents: () => mockUseIncidents(),
	};
});

describe("Alerts", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		Object.defineProperty(globalThis, "confirm", {
			value: vi.fn(() => true),
			configurable: true,
		});

		mockCreateChannel.mockResolvedValue({ id: "channel-1" });
		mockUpdateChannel.mockResolvedValue(undefined);
		mockDeleteChannel.mockResolvedValue(undefined);
		mockTestChannel.mockResolvedValue({ status: "test sent" });
		mockCreateRule.mockResolvedValue({ id: "rule-1" });
		mockUpdateRule.mockResolvedValue(undefined);
		mockDeleteRule.mockResolvedValue(undefined);
		mockAcknowledgeIncident.mockResolvedValue(undefined);
		mockRefetchChannels.mockResolvedValue(undefined);
		mockRefetchRules.mockResolvedValue(undefined);
		mockRefetchIncidents.mockResolvedValue(undefined);

		mockUseChannels.mockReturnValue({
			channels: [],
			loading: false,
			error: null,
			refetch: mockRefetchChannels,
			createChannel: mockCreateChannel,
			updateChannel: mockUpdateChannel,
			deleteChannel: mockDeleteChannel,
			testChannel: mockTestChannel,
		});
		mockUseRules.mockReturnValue({
			rules: [],
			loading: false,
			error: null,
			refetch: mockRefetchRules,
			createRule: mockCreateRule,
			updateRule: mockUpdateRule,
			deleteRule: mockDeleteRule,
		});
		mockUseIncidents.mockReturnValue({
			incidents: [],
			loading: false,
			error: null,
			refetch: mockRefetchIncidents,
			acknowledgeIncident: mockAcknowledgeIncident,
		});
	});

	it("creates a Discord channel with backend dispatcher config", async () => {
		render(<Alerts />);

		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
		fireEvent.change(screen.getByPlaceholderText("e.g., Ops Slack"), {
			target: { value: "Ops Discord" },
		});
		fireEvent.click(screen.getByRole("button", { name: "Discord" }));
		fireEvent.change(
			screen.getByPlaceholderText("https://discord.com/api/webhooks/..."),
			{
				target: { value: "https://discord.com/api/webhooks/test" },
			},
		);
		fireEvent.click(
			screen.getAllByRole("button", { name: "Add Channel" }).at(-1)!,
		);

		await waitFor(() => {
			expect(mockCreateChannel).toHaveBeenCalledWith({
				name: "Ops Discord",
				type: "discord",
				enabled: true,
				config: { webhook_url: "https://discord.com/api/webhooks/test" },
			});
		});
	});

	it("creates an email channel with SMTP and recipient config", async () => {
		render(<Alerts />);

		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
		fireEvent.change(screen.getByPlaceholderText("e.g., Ops Slack"), {
			target: { value: "Ops Email" },
		});
		fireEvent.click(screen.getByRole("button", { name: "Email" }));
		fireEvent.change(screen.getByPlaceholderText("smtp.example.com"), {
			target: { value: "smtp.example.com" },
		});
		fireEvent.change(screen.getByPlaceholderText("alerts@example.com"), {
			target: { value: "alerts@example.com" },
		});
		fireEvent.change(
			screen.getByPlaceholderText("ops@example.com, oncall@example.com"),
			{
				target: { value: "ops@example.com, oncall@example.com" },
			},
		);
		fireEvent.click(
			screen.getAllByRole("button", { name: "Add Channel" }).at(-1)!,
		);

		await waitFor(() => {
			expect(mockCreateChannel).toHaveBeenCalledWith({
				name: "Ops Email",
				type: "email",
				enabled: true,
				config: {
					smtp_host: "smtp.example.com",
					smtp_port: 587,
					from: "alerts@example.com",
					to: ["ops@example.com", "oncall@example.com"],
				},
			});
		});
	});

	it("shows channel validation errors and supports testing, toggling, editing, and deleting channels", async () => {
		mockUseChannels.mockReturnValue({
			channels: [
				{
					id: "channel-1",
					name: "PagerDuty Primary",
					type: "pagerduty",
					enabled: true,
					config: { integration_key: "pd-key" },
				},
			],
			loading: false,
			error: null,
			refetch: mockRefetchChannels,
			createChannel: mockCreateChannel,
			updateChannel: mockUpdateChannel,
			deleteChannel: mockDeleteChannel,
			testChannel: mockTestChannel,
		});

		render(<Alerts />);
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));

		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
		fireEvent.click(screen.getByRole("button", { name: "Slack" }));
		fireEvent.click(
			screen.getAllByRole("button", { name: "Add Channel" }).at(-1)!,
		);
		expect(mockCreateChannel).not.toHaveBeenCalled();

		fireEvent.click(screen.getByLabelText(/test channel pagerduty primary/i));
		await waitFor(() =>
			expect(mockTestChannel).toHaveBeenCalledWith("channel-1"),
		);
		expect(
			await screen.findByText("Test notification sent successfully!"),
		).toBeInTheDocument();

		fireEvent.click(screen.getByLabelText(/disable pagerduty primary/i));
		await waitFor(() =>
			expect(mockUpdateChannel).toHaveBeenCalledWith("channel-1", {
				enabled: false,
			}),
		);

		fireEvent.click(screen.getByLabelText(/edit channel pagerduty primary/i));
		const dialog = screen.getByRole("dialog");
		expect(
			within(dialog).getByDisplayValue("PagerDuty Primary"),
		).toBeInTheDocument();
		fireEvent.change(within(dialog).getByDisplayValue("PagerDuty Primary"), {
			target: { value: "PagerDuty Secondary" },
		});
		fireEvent.click(
			screen.getAllByRole("button", { name: "Save Channel" }).at(-1)!,
		);
		await waitFor(() => {
			expect(mockUpdateChannel).toHaveBeenCalledWith(
				"channel-1",
				expect.objectContaining({
					name: "PagerDuty Secondary",
					type: "pagerduty",
				}),
			);
		});

		fireEvent.click(screen.getByLabelText(/delete channel pagerduty primary/i));
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /confirm/i }));
		await waitFor(() =>
			expect(mockDeleteChannel).toHaveBeenCalledWith("channel-1"),
		);
	});

	it("creates rules, toggles and deletes them, and shows empty and error states", async () => {
		mockUseChannels.mockReturnValue({
			channels: [
				{
					id: "channel-1",
					name: "Ops Email",
					type: "email",
					enabled: true,
					config: {},
				},
			],
			loading: false,
			error: null,
			refetch: mockRefetchChannels,
			createChannel: mockCreateChannel,
			updateChannel: mockUpdateChannel,
			deleteChannel: mockDeleteChannel,
			testChannel: mockTestChannel,
		});
		mockUseRules.mockReturnValue({
			rules: [
				{
					id: "rule-1",
					name: "High Latency",
					condition: "response_time",
					threshold: 5000,
					severity: "critical",
					consecutive: 3,
					duration: 60,
					enabled: true,
					channels: ["channel-1"],
					created_at: "2026-07-06T00:00:00Z",
				},
			],
			loading: false,
			error: null,
			refetch: mockRefetchRules,
			createRule: mockCreateRule,
			updateRule: mockUpdateRule,
			deleteRule: mockDeleteRule,
		});

		render(<Alerts />);
		expect(screen.getByText("High Latency")).toBeInTheDocument();

		fireEvent.click(screen.getByLabelText(/disable rule high latency/i));
		await waitFor(() =>
			expect(mockUpdateRule).toHaveBeenCalledWith("rule-1", { enabled: false }),
		);

		fireEvent.click(screen.getByLabelText(/edit rule high latency/i));
		const dialog = screen.getByRole("dialog");
		fireEvent.change(
			within(dialog).getByPlaceholderText("e.g., High Latency"),
			{ target: { value: "Latency Warning" } },
		);
		fireEvent.click(
			screen.getAllByRole("button", { name: "Save Rule" }).at(-1)!,
		);
		await waitFor(() => {
			expect(mockUpdateRule).toHaveBeenCalledWith(
				"rule-1",
				expect.objectContaining({
					name: "Latency Warning",
					channels: ["channel-1"],
				}),
			);
		});

		fireEvent.click(screen.getByLabelText(/delete rule/i));
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
		expect(mockDeleteRule).not.toHaveBeenCalled();
		fireEvent.click(screen.getByLabelText(/delete rule/i));
		await screen.findByRole("dialog");
		fireEvent.click(screen.getByRole("button", { name: /confirm/i }));
		await waitFor(() => expect(mockDeleteRule).toHaveBeenCalledWith("rule-1"));

		mockUseRules.mockReturnValue({
			rules: [],
			loading: false,
			error: "rules unavailable",
			refetch: mockRefetchRules,
			createRule: mockCreateRule,
			updateRule: mockUpdateRule,
			deleteRule: mockDeleteRule,
		});

		render(<Alerts />);
		expect(screen.getByText("rules unavailable")).toBeInTheDocument();
	});

	it("renders history states and acknowledges open incidents", async () => {
		mockUseIncidents.mockReturnValue({
			incidents: [
				{
					id: "incident-1",
					rule_id: "rule-1",
					soul_id: "soul-1",
					soul_name: "Payments API",
					severity: "critical",
					status: "open",
					started_at: "2026-07-06T10:00:00Z",
				},
			],
			loading: false,
			error: null,
			refetch: mockRefetchIncidents,
			acknowledgeIncident: mockAcknowledgeIncident,
		});

		render(<Alerts />);
		fireEvent.click(screen.getByRole("tab", { name: /history/i }));
		expect(screen.getByText("Incident incident-1")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /acknowledge/i }));
		await waitFor(() =>
			expect(mockAcknowledgeIncident).toHaveBeenCalledWith("incident-1"),
		);
	});

	it("routes refresh actions and filters rules by search and severity", async () => {
		mockUseRules.mockReturnValue({ rules: [
			{ id: "r1", name: "Warning Rule", condition: "error_rate", threshold: 2, severity: "warning", enabled: false, channels: ["missing"] },
			{ id: "r2", name: "Info Rule", condition: "downtime", threshold: 1, severity: "info", enabled: true, channels: [] },
		], loading: true, error: null, refetch: mockRefetchRules, createRule: mockCreateRule, updateRule: mockUpdateRule, deleteRule: mockDeleteRule });
		render(<Alerts />);
		fireEvent.click(screen.getByLabelText("Refresh"));
		await waitFor(() => expect(mockRefetchRules).toHaveBeenCalled());
		fireEvent.change(screen.getByPlaceholderText("Search alert rules..."), { target: { value: "warning" } });
		fireEvent.change(screen.getByDisplayValue("All Severities"), { target: { value: "warning" } });
		expect(screen.getByText("Warning Rule")).toBeInTheDocument();
		expect(screen.queryByText("Info Rule")).not.toBeInTheDocument();
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getByLabelText("Refresh"));
		await waitFor(() => expect(mockRefetchChannels).toHaveBeenCalled());
		fireEvent.click(screen.getByRole("tab", { name: /history/i }));
		fireEvent.click(screen.getByLabelText("Refresh"));
		await waitFor(() => expect(mockRefetchIncidents).toHaveBeenCalled());
	});

	it("creates webhook, Slack, and PagerDuty channels and exercises modal controls", async () => {
		render(<Alerts />);
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		for (const [type, placeholder, value, config] of [
			["Webhook", "https://example.com/webhook", "https://example.com/hook", { url: "https://example.com/hook" }],
			["Slack", "https://hooks.slack.com/services/...", "https://hooks.slack/x", { webhook_url: "https://hooks.slack/x" }],
			["PagerDuty", "PagerDuty Events API key", "key", { integration_key: "key" }],
		] as const) {
			fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
			fireEvent.change(screen.getByPlaceholderText("e.g., Ops Slack"), { target: { value: `${type} Ops` } });
			fireEvent.click(screen.getByRole("button", { name: type }));
			fireEvent.change(screen.getByPlaceholderText(placeholder), { target: { value } });
			fireEvent.click(screen.getByLabelText("Enabled"));
			fireEvent.click(screen.getAllByRole("button", { name: "Add Channel" }).at(-1)!);
			await waitFor(() => expect(mockCreateChannel).toHaveBeenLastCalledWith(expect.objectContaining({ type: type.toLowerCase() === "pagerduty" ? "pagerduty" : type.toLowerCase(), config, enabled: false })));
		}
		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
		fireEvent.keyDown(screen.getByRole("dialog"), { key: "Enter" });
		fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});

	it("validates every channel type and displays save failures", async () => {
		render(<Alerts />);
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i })[0]);
		const name = screen.getByPlaceholderText("e.g., Ops Slack");
		for (const type of ["Slack", "Discord", "PagerDuty", "Email"]) {
			fireEvent.change(name, { target: { value: "Channel" } });
			fireEvent.click(screen.getByRole("button", { name: type }));
			expect(screen.getAllByRole("button", { name: "Add Channel" }).at(-1)).toBeDisabled();
		}
		fireEvent.change(screen.getByPlaceholderText("smtp.example.com"), { target: { value: "smtp" } });
		fireEvent.change(screen.getByPlaceholderText("alerts@example.com"), { target: { value: "from@example.com" } });
		expect(screen.getAllByRole("button", { name: "Add Channel" }).at(-1)).toBeDisabled();
		fireEvent.change(screen.getByPlaceholderText("ops@example.com, oncall@example.com"), { target: { value: "to@example.com" } });
		mockCreateChannel.mockRejectedValueOnce(new Error("save failed"));
		fireEvent.click(screen.getAllByRole("button", { name: "Add Channel" }).at(-1)!);
		expect(await screen.findByText("save failed")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
	});

	it("covers channel edit config fallbacks, test outcomes, and delete cancellation", async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		const channels = [
			{ id: "c1", name: "Email", type: "email", enabled: false, config: { to: ["one@example.com", 42], smtp_port: "2525", smtp_host: "smtp", from: "from" } },
			{ id: "c2", name: "Unknown", type: "unknown", enabled: false, config: {} },
		];
		mockUseChannels.mockReturnValue({ channels, loading: false, error: "channels unavailable", refetch: mockRefetchChannels, createChannel: mockCreateChannel, updateChannel: mockUpdateChannel, deleteChannel: mockDeleteChannel, testChannel: mockTestChannel });
		mockTestChannel.mockResolvedValueOnce({ status: "rejected" }).mockRejectedValueOnce("bad test");
		render(<Alerts />);
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getByRole("button", { name: "Try Again" }));
		fireEvent.click(screen.getByLabelText(/test channel email/i));
		expect(await screen.findByText("Test failed: rejected")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText(/test channel unknown/i));
		expect(await screen.findByText("Test failed")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText(/edit channel email/i));
		expect(screen.getByDisplayValue("2525")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText("Close dialog"));
		(globalThis.confirm as ReturnType<typeof vi.fn>).mockReturnValueOnce(false);
		fireEvent.click(screen.getByLabelText(/delete channel email/i));
		expect(mockDeleteChannel).not.toHaveBeenCalled();
		await act(async () => { await vi.runAllTimersAsync(); });
		vi.useRealTimers();
	});

	it("creates rules, rejects missing channels, handles save failure, and closes with Escape", async () => {
		render(<Alerts />);
		fireEvent.click(screen.getByRole("button", { name: /add rule/i }));
		expect(screen.getAllByRole("button", { name: "Add Rule" }).at(-1)!).toBeDisabled();
		fireEvent.change(screen.getByPlaceholderText("e.g., High Latency"), { target: { value: "Rule" } });
		expect(screen.getAllByRole("button", { name: "Add Rule" }).at(-1)!).toBeDisabled();
		fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
		cleanup();
		mockUseChannels.mockReturnValue({ channels: [{ id: "c1", name: "Ops", type: "webhook", enabled: true, config: {} }], loading: false, error: null, refetch: mockRefetchChannels, createChannel: mockCreateChannel, updateChannel: mockUpdateChannel, deleteChannel: mockDeleteChannel, testChannel: mockTestChannel });
		mockCreateRule.mockRejectedValueOnce("rule failed");
		render(<Alerts />);
		fireEvent.click(screen.getByRole("button", { name: /add rule/i }));
		fireEvent.change(screen.getByPlaceholderText("e.g., High Latency"), { target: { value: "Rule" } });
		fireEvent.change(screen.getByDisplayValue("Response Time > threshold"), { target: { value: "downtime" } });
		for (const label of ["Warning", "Info"]) fireEvent.click(screen.getByRole("button", { name: label }));
		const numbers = screen.getByRole("dialog").querySelectorAll<HTMLInputElement>('input[type="number"]');
		for (const input of numbers) fireEvent.change(input, { target: { value: "" } });
		fireEvent.click(screen.getByLabelText("Enabled"));
		fireEvent.click(screen.getAllByRole("button", { name: "Add Rule" }).at(-1)!);
		expect(await screen.findByText("Failed to save rule")).toBeInTheDocument();
		fireEvent.click(screen.getByLabelText("Close dialog"));
	});

	it("opens modals from empty and card actions and updates SMTP port", () => {
		render(<Alerts />);
		fireEvent.click(screen.getByRole("button", { name: "Create Rule" }));
		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
		fireEvent.click(screen.getByRole("tab", { name: /channels/i }));
		fireEvent.click(screen.getAllByRole("button", { name: /add channel/i }).at(-1)!);
		fireEvent.change(screen.getByPlaceholderText("e.g., Ops Slack"), { target: { value: "Email" } });
		fireEvent.click(screen.getByRole("button", { name: "Email" }));
		const port = screen.getByRole("dialog").querySelector<HTMLInputElement>('input[type="number"]')!;
		fireEvent.change(port, { target: { value: "2525" } });
		expect(port).toHaveValue(2525);
		fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
	});

	it("renders history loading, error, empty, resolved, and acknowledged states", () => {
		for (const state of [
			{ incidents: [], loading: true, error: null },
			{ incidents: [], loading: false, error: "history failed" },
			{ incidents: [], loading: false, error: null },
			{ incidents: [
				{ id: "resolved", rule_id: "r", soul_id: "s", severity: "warning", status: "resolved", started_at: "2026-01-01" },
				{ id: "ack", rule_id: "r", soul_id: "s2", severity: "info", status: "acknowledged", started_at: "2026-01-01" },
			], loading: false, error: null },
		]) {
			mockUseIncidents.mockReturnValue({ ...state, refetch: mockRefetchIncidents, acknowledgeIncident: mockAcknowledgeIncident });
			render(<Alerts />);
			fireEvent.click(screen.getAllByRole("tab", { name: /history/i }).at(-1)!);
		}
		expect(screen.getByText("Incident resolved")).toBeInTheDocument();
		expect(screen.getByText("Incident ack")).toBeInTheDocument();
	});
});
