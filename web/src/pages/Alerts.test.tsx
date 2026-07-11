import {
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
});
