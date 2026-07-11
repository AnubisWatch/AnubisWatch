import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

function mockJsonResponse(body: unknown, status = 200, ok = true) {
	return {
		ok,
		status,
		json: () => Promise.resolve(body),
	};
}

describe("ApiClient extra serialization and normalization", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.restoreAllMocks();
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("adds default config for udp, smtp, icmp, grpc, websocket, and tls souls", async () => {
		global.fetch = vi
			.fn()
			.mockResolvedValue(mockJsonResponse({ id: "ok" }, 201)) as typeof fetch;

		await api.post("/souls", {
			name: "UDP",
			type: "udp",
			target: "udp.example.com:53",
			enabled: true,
			weight: 30,
			timeout: 5,
		});
		await api.post("/souls", {
			name: "SMTP",
			type: "smtp",
			target: "mail.example.com:587",
			enabled: true,
			weight: 30,
			timeout: 5,
		});
		await api.post("/souls", {
			name: "ICMP",
			type: "icmp",
			target: "1.1.1.1",
			enabled: true,
			weight: 30,
			timeout: 5,
		});
		await api.post("/souls", {
			name: "gRPC",
			type: "grpc",
			target: "grpc.example.com:443",
			enabled: true,
			weight: 30,
			timeout: 5,
		});
		await api.post("/souls", {
			name: "WS",
			type: "websocket",
			target: "wss://stream.example.com",
			enabled: true,
			weight: 30,
			timeout: 5,
		});
		await api.post("/souls", {
			name: "TLS",
			type: "tls",
			target: "tls.example.com:443",
			enabled: true,
			weight: 30,
			timeout: 5,
		});

		const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map(
			(call) => JSON.parse(call[1].body),
		);
		expect(calls[0]).toMatchObject({ udp: {} });
		expect(calls[1]).toMatchObject({ smtp: {} });
		expect(calls[2]).toMatchObject({
			icmp: { count: 4, interval: "1s", max_loss_percent: 100 },
		});
		expect(calls[3]).toMatchObject({ grpc: { metadata: {} } });
		expect(calls[4]).toMatchObject({
			websocket: { headers: {}, ping_check: true },
		});
		expect(calls[5]).toMatchObject({
			tls: { expiry_warn_days: 30, expiry_critical_days: 7 },
		});
	});

	it("serializes alternate alert-rule conditions", async () => {
		global.fetch = vi
			.fn()
			.mockResolvedValue(mockJsonResponse({ id: "ok" }, 201)) as typeof fetch;

		await api.post("/rules", {
			name: "Error Rate",
			condition: "error_rate",
			threshold: 55,
			duration: 120,
			consecutive: 1,
			severity: "critical",
			enabled: true,
			channels: ["channel-1"],
		});
		await api.post("/rules", {
			name: "Downtime",
			condition: "downtime",
			threshold: 0,
			duration: 300,
			consecutive: 4,
			severity: "critical",
			enabled: true,
			channels: ["channel-1"],
		});
		await api.post("/rules", {
			name: "SSL",
			condition: "ssl_expiry",
			threshold: 14,
			duration: 600,
			consecutive: 1,
			severity: "warning",
			enabled: true,
			channels: ["channel-1"],
		});

		const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map(
			(call) => JSON.parse(call[1].body),
		);
		expect(calls[0].conditions[0]).toEqual({
			type: "failure_rate",
			threshold: 55,
			window: "120s",
		});
		expect(calls[1].conditions[0]).toEqual({
			type: "consecutive_failures",
			threshold: 4,
			status: "dead",
			window: "300s",
		});
		expect(calls[2].conditions[0]).toEqual({
			type: "threshold",
			metric: "tls_expiry_days",
			operator: "<",
			value: 14,
			window: "600s",
		});
	});

	it("serializes light status page themes and preserves existing conditions arrays", async () => {
		global.fetch = vi
			.fn()
			.mockResolvedValue(mockJsonResponse({ id: "ok" }, 201)) as typeof fetch;

		await api.post("/status-pages", {
			name: "Light Page",
			slug: "light-page",
			description: "Public status",
			theme: "light",
			enabled: true,
			souls: [],
			uptime_days: 30,
		});

		await api.post("/rules", {
			name: "Prebuilt",
			severity: "warning",
			enabled: true,
			channels: ["channel-1"],
			conditions: [
				{
					type: "threshold",
					metric: "latency_ms",
					operator: ">",
					value: 1000,
					window: "60s",
				},
			],
		});

		const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map(
			(call) => JSON.parse(call[1].body),
		);
		expect(calls[0].theme).toEqual({
			primary_color: "#d97706",
			background_color: "#ffffff",
			text_color: "#111827",
			accent_color: "#0d9488",
			font_family: "system-ui, -apple-system, sans-serif",
		});
		expect(calls[1].conditions).toEqual([
			{
				type: "threshold",
				metric: "latency_ms",
				operator: ">",
				value: 1000,
				window: "60s",
			},
		]);
	});

	it("normalizes extra backend soul and status-page variants", async () => {
		global.fetch = vi
			.fn()
			.mockResolvedValueOnce(
				mockJsonResponse({
					data: [
						{
							id: "soul-1",
							name: "WS",
							type: "websocket",
							target: "wss://stream.example.com",
							weight: "45s",
							timeout: "90s",
							status: "dead",
							websocket: { ping_check: false },
						},
						{
							id: "soul-2",
							name: "ICMP",
							type: "icmp",
							target: "1.1.1.1",
							weight: "15s",
							timeout: "5s",
							status: "unknown",
						},
					],
				}),
			)
			.mockResolvedValueOnce(
				mockJsonResponse([
					{
						id: "page-1",
						slug: "light",
						enabled: true,
						souls: [],
						theme: { background_color: "#ffffff" },
						custom_domain: "status.example.com",
					},
					{
						id: "page-2",
						slug: "dark",
						enabled: true,
						souls: [],
						theme: { background_color: "#0f172a" },
					},
				]),
			) as typeof fetch;

		const souls = await api.get<{
			data: Array<{
				status: string;
				weight: number;
				timeout: number;
				websocket?: unknown;
			}>;
		}>("/souls");
		expect(souls.data[0]).toMatchObject({
			status: "unhealthy",
			weight: 45,
			timeout: 90,
			websocket: { ping_check: false },
		});
		expect(souls.data[1]).toMatchObject({
			status: "unknown",
			weight: 15,
			timeout: 5,
		});

		const pages =
			await api.get<Array<{ theme: string; domain?: string }>>("/status-pages");
		expect(pages[0]).toMatchObject({
			theme: "light",
			domain: "status.example.com",
		});
		expect(pages[1]).toMatchObject({ theme: "dark" });
	});
});
