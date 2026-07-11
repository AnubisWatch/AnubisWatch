import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { defaultSoulFormData, type SoulFormData } from "../utils/soulForm";
import { SoulProtocolFields } from "./SoulProtocolFields";

function renderFields(type: SoulFormData["type"]) {
	const setFormData = vi.fn();
	const formData = { ...defaultSoulFormData, type };
	render(<SoulProtocolFields formData={formData} setFormData={setFormData} />);
	return { setFormData, formData };
}

describe("SoulProtocolFields", () => {
	it("renders and updates HTTP fields", () => {
		const { setFormData, formData } = renderFields("http");

		fireEvent.change(screen.getByLabelText("HTTP Method"), {
			target: { value: "POST" },
		});
		fireEvent.change(screen.getByLabelText("Valid Status Codes"), {
			target: { value: "200,201" },
		});

		expect(setFormData).toHaveBeenCalledWith({
			...formData,
			httpMethod: "POST",
		});
		expect(setFormData).toHaveBeenCalledWith({
			...formData,
			httpValidStatus: "200,201",
		});
	});

	it("renders TCP and UDP field groups", () => {
		const tcp = renderFields("tcp");
		fireEvent.change(screen.getByLabelText("Send Text"), {
			target: { value: "PING" },
		});
		fireEvent.change(screen.getByLabelText("Expected Banner Regex"), {
			target: { value: "^PONG" },
		});
		expect(tcp.setFormData).toHaveBeenCalledWith({
			...tcp.formData,
			tcpSend: "PING",
		});
		expect(tcp.setFormData).toHaveBeenCalledWith({
			...tcp.formData,
			tcpExpectRegex: "^PONG",
		});

		cleanup();
	});

	it("renders DNS, ICMP, SMTP, gRPC, WebSocket, and TLS fields", () => {
		const dns = renderFields("dns");
		fireEvent.change(screen.getByLabelText("DNS Record Type"), {
			target: { value: "TXT" },
		});
		fireEvent.change(screen.getByLabelText("Expected DNS Values"), {
			target: { value: "v=spf1" },
		});
		expect(dns.setFormData).toHaveBeenCalledWith({
			...dns.formData,
			dnsRecordType: "TXT",
		});
		expect(dns.setFormData).toHaveBeenCalledWith({
			...dns.formData,
			dnsExpected: "v=spf1",
		});

		document.body.innerHTML = "";
		const icmp = renderFields("icmp");
		fireEvent.change(screen.getByLabelText("ICMP Count"), {
			target: { value: "4" },
		});
		fireEvent.change(screen.getByLabelText("Interval Seconds"), {
			target: { value: "2" },
		});
		fireEvent.change(screen.getByLabelText("Max Loss Percent"), {
			target: { value: "25" },
		});
		expect(icmp.setFormData).toHaveBeenCalledWith({
			...icmp.formData,
			icmpInterval: 2,
		});
		expect(icmp.setFormData).toHaveBeenCalledWith({
			...icmp.formData,
			icmpMaxLossPercent: 25,
		});

		document.body.innerHTML = "";
		const smtp = renderFields("smtp");
		fireEvent.click(screen.getByLabelText("Require STARTTLS"));
		fireEvent.change(screen.getByLabelText("Expected SMTP Banner"), {
			target: { value: "ESMTP" },
		});
		expect(smtp.setFormData).toHaveBeenCalledWith({
			...smtp.formData,
			smtpStartTLS: false,
		});
		expect(smtp.setFormData).toHaveBeenCalledWith({
			...smtp.formData,
			smtpBannerContains: "ESMTP",
		});

		document.body.innerHTML = "";
		const grpc = renderFields("grpc");
		fireEvent.change(screen.getByLabelText("gRPC Service Name"), {
			target: { value: "grpc.health.v1.Health" },
		});
		expect(grpc.setFormData).toHaveBeenCalledWith({
			...grpc.formData,
			grpcService: "grpc.health.v1.Health",
		});

		document.body.innerHTML = "";
		const websocket = renderFields("websocket");
		fireEvent.click(screen.getByLabelText("Send WebSocket ping"));
		fireEvent.change(screen.getByLabelText("Send Message"), {
			target: { value: "ping" },
		});
		fireEvent.change(screen.getByLabelText("Expected Message Text"), {
			target: { value: "pong" },
		});
		expect(websocket.setFormData).toHaveBeenCalledWith({
			...websocket.formData,
			websocketPingCheck: false,
		});
		expect(websocket.setFormData).toHaveBeenCalledWith({
			...websocket.formData,
			websocketSend: "ping",
		});
		expect(websocket.setFormData).toHaveBeenCalledWith({
			...websocket.formData,
			websocketExpectContains: "pong",
		});

		document.body.innerHTML = "";
		const tls = renderFields("tls");
		fireEvent.change(screen.getByLabelText("Expiry Warning Days"), {
			target: { value: "21" },
		});
		fireEvent.change(screen.getByLabelText("Expiry Critical Days"), {
			target: { value: "7" },
		});
		expect(tls.setFormData).toHaveBeenCalledWith({
			...tls.formData,
			tlsExpiryWarnDays: 21,
		});
	});
});
