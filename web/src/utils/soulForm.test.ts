import { describe, expect, it } from "vitest";
import {
	buildSoulPayload,
	defaultSoulFormData,
	nextSoulFormDataForType,
	soulFormDataFromSoul,
} from "./soulForm";

describe("soulForm helpers", () => {
	it("resets protocol-specific fields when switching types", () => {
		const next = nextSoulFormDataForType(
			{
				...defaultSoulFormData,
				name: "API",
				enabled: false,
				weight: 120,
				timeout: 25,
				tags: ["prod"],
				httpMethod: "POST",
				httpValidStatus: "200,201",
			},
			"dns",
		);

		expect(next).toMatchObject({
			name: "API",
			type: "dns",
			enabled: false,
			weight: 120,
			timeout: 25,
			tags: ["prod"],
			httpMethod: "GET",
			dnsRecordType: "A",
		});
	});

	it("hydrates form data from souls with legacy and modern protocol config", () => {
		const soul = {
			id: "soul-1",
			name: "TLS Edge",
			type: "tls" as const,
			target: "edge.example.com:443",
			enabled: true,
			weight: 90,
			timeout: 15,
			tags: ["edge"],
			http_config: { method: "HEAD", valid_status: [200, 204] },
			dns_config: { record_type: "AAAA", expected: ["2001:db8::1"] },
			icmp: { count: 2, interval: "5", max_loss_percent: 40 },
			tls: { expiry_warn_days: 14, expiry_critical_days: 3 },
			created_at: "",
			updated_at: "",
		};

		const form = soulFormDataFromSoul(soul);
		expect(form.httpMethod).toBe("HEAD");
		expect(form.httpValidStatus).toBe("200, 204");
		expect(form.dnsRecordType).toBe("AAAA");
		expect(form.dnsExpected).toBe("2001:db8::1");
		expect(form.icmpCount).toBe(2);
		expect(form.icmpInterval).toBe(5);
		expect(form.icmpMaxLossPercent).toBe(40);
		expect(form.tlsExpiryWarnDays).toBe(14);
		expect(form.tlsExpiryCriticalDays).toBe(3);
	});

	it("hydrates defaults when optional soul fields and configs are absent", () => {
		const form = soulFormDataFromSoul({
			id: "minimal",
			name: undefined as unknown as string,
			type: undefined as unknown as "http",
			target: undefined as unknown as string,
			created_at: "",
			updated_at: "",
		});

		expect(form).toMatchObject(defaultSoulFormData);
	});

	it("hydrates modern configs and falls back from invalid numeric fields", () => {
		const form = soulFormDataFromSoul({
			id: "modern",
			name: "Modern",
			type: "tcp",
			target: "host:1",
			enabled: false,
			weight: 0,
			timeout: 0,
			tags: [],
			http: { method: "PUT", valid_status: [201] },
			tcp: { send: "x", expect_regex: "y" },
			dns: { record_type: "MX", expected: ["mail"] },
			icmp: { interval: "invalid" },
			created_at: "",
			updated_at: "",
		});

		expect(form).toMatchObject({
			enabled: false,
			weight: 0,
			timeout: 0,
			httpMethod: "PUT",
			httpValidStatus: "201",
			tcpSend: "x",
			tcpExpectRegex: "y",
			dnsRecordType: "MX",
			dnsExpected: "mail",
			icmpInterval: 1,
		});
	});

	it("builds protocol-specific payloads for all supported soul types", () => {
		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "HTTP",
				type: "http",
				target: "https://example.com",
				httpMethod: "POST",
				httpValidStatus: "200 204",
			}),
		).toMatchObject({
			http: { method: "POST", valid_status: [200, 204] },
			workspace_id: "default",
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "TCP",
				type: "tcp",
				target: "tcp.example.com:443",
				tcpSend: "PING",
				tcpExpectRegex: "^PONG$",
			}),
		).toMatchObject({
			tcp: { send: "PING", expect_regex: "^PONG$" },
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "UDP",
				type: "udp",
				target: "udp.example.com:53",
				udpSendHex: "AAFF",
				udpExpectContains: "ok",
			}),
		).toMatchObject({
			udp: { send_hex: "AAFF", expect_contains: "ok" },
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "DNS",
				type: "dns",
				target: "example.com",
				dnsRecordType: "TXT",
				dnsExpected: "v=spf1, include:_spf.example.com",
			}),
		).toMatchObject({
			dns: {
				record_type: "TXT",
				expected: ["v=spf1", "include:_spf.example.com"],
			},
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "ICMP",
				type: "icmp",
				target: "1.1.1.1",
				icmpCount: 3,
				icmpInterval: 2,
				icmpMaxLossPercent: 25,
			}),
		).toMatchObject({
			icmp: { count: 3, interval: "2s", max_loss_percent: 25 },
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "SMTP",
				type: "smtp",
				target: "mail.example.com:587",
				smtpStartTLS: false,
				smtpBannerContains: "ESMTP",
			}),
		).toMatchObject({
			smtp: { starttls: false, banner_contains: "ESMTP" },
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "gRPC",
				type: "grpc",
				target: "grpc.example.com:443",
				grpcService: "grpc.health.v1.Health",
			}),
		).toMatchObject({
			grpc: { service: "grpc.health.v1.Health", metadata: {} },
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "WS",
				type: "websocket",
				target: "wss://stream.example.com",
				websocketPingCheck: false,
				websocketSend: "ping",
				websocketExpectContains: "pong",
			}),
		).toMatchObject({
			websocket: {
				headers: {},
				ping_check: false,
				send: "ping",
				expect_contains: "pong",
			},
		});

		expect(
			buildSoulPayload({
				...defaultSoulFormData,
				name: "TLS",
				type: "tls",
				target: "tls.example.com:443",
				tlsExpiryWarnDays: 21,
				tlsExpiryCriticalDays: 5,
			}),
		).toMatchObject({
			tls: { expiry_warn_days: 21, expiry_critical_days: 5 },
		});
	});

	it("uses payload fallbacks for invalid numbers, lists, and empty protocol options", () => {
		const invalid = { ...defaultSoulFormData, weight: Number.NaN, timeout: Number.NaN };
		expect(buildSoulPayload({ ...invalid, type: "http", httpValidStatus: "0, no" })).toMatchObject({ weight: 60, timeout: 10, http: { valid_status: [200] } });
		expect(buildSoulPayload({ ...invalid, type: "tcp" }).tcp).toEqual({ send: undefined, expect_regex: undefined });
		expect(buildSoulPayload({ ...invalid, type: "udp" }).udp).toEqual({ send_hex: undefined, expect_contains: undefined });
		expect(buildSoulPayload({ ...invalid, type: "smtp" }).smtp).toEqual({ starttls: true, banner_contains: undefined });
		expect(buildSoulPayload({ ...invalid, type: "grpc" }).grpc).toEqual({ service: undefined, metadata: {} });
		expect(buildSoulPayload({ ...invalid, type: "websocket" }).websocket).toEqual({ headers: {}, ping_check: true, send: undefined, expect_contains: undefined });
		expect(buildSoulPayload({ ...invalid, type: "icmp", icmpCount: Number.NaN, icmpInterval: Number.NaN, icmpMaxLossPercent: Number.NaN }).icmp).toEqual({ count: 4, interval: "1s", max_loss_percent: 100 });
		expect(buildSoulPayload({ ...invalid, type: "tls", tlsExpiryWarnDays: Number.NaN, tlsExpiryCriticalDays: Number.NaN }, "ops")).toMatchObject({ workspace_id: "ops", tls: { expiry_warn_days: 30, expiry_critical_days: 7 } });
	});
});
