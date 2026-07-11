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
});
