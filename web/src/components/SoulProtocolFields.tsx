import { soulTypeOptions, type SoulFormData } from '../utils/soulForm'

interface SoulProtocolFieldsProps {
  formData: SoulFormData
  setFormData: (data: SoulFormData) => void
}

export function SoulProtocolFields({ formData, setFormData }: SoulProtocolFieldsProps) {
  const typeLabel = soulTypeOptions.find((option) => option.value === formData.type)?.label || formData.type.toUpperCase()

  return (
    <div className="rounded-xl border border-gray-700/60 bg-gray-950/50 p-4 space-y-4">
      <h3 className="text-sm font-semibold text-white">{typeLabel} Settings</h3>

      {formData.type === 'http' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label htmlFor="http-method" className="block text-sm font-medium text-gray-300 mb-2">HTTP Method</label>
            <select
              id="http-method"
              value={formData.httpMethod}
              onChange={(e) => setFormData({ ...formData, httpMethod: e.target.value })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            >
              {['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map((method) => (
                <option key={method} value={method}>{method}</option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="http-valid-status" className="block text-sm font-medium text-gray-300 mb-2">Valid Status Codes</label>
            <input
              id="http-valid-status"
              type="text"
              value={formData.httpValidStatus}
              onChange={(e) => setFormData({ ...formData, httpValidStatus: e.target.value })}
              placeholder="200, 204"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'tcp' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label htmlFor="tcp-send" className="block text-sm font-medium text-gray-300 mb-2">Send Text</label>
            <input
              id="tcp-send"
              type="text"
              value={formData.tcpSend}
              onChange={(e) => setFormData({ ...formData, tcpSend: e.target.value })}
              placeholder="Optional payload"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label htmlFor="tcp-expect" className="block text-sm font-medium text-gray-300 mb-2">Expected Banner Regex</label>
            <input
              id="tcp-expect"
              type="text"
              value={formData.tcpExpectRegex}
              onChange={(e) => setFormData({ ...formData, tcpExpectRegex: e.target.value })}
              placeholder="^220"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'udp' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label htmlFor="udp-send-hex" className="block text-sm font-medium text-gray-300 mb-2">UDP Send Hex</label>
            <input
              id="udp-send-hex"
              type="text"
              value={formData.udpSendHex}
              onChange={(e) => setFormData({ ...formData, udpSendHex: e.target.value })}
              placeholder="Optional hex payload"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label htmlFor="udp-expect" className="block text-sm font-medium text-gray-300 mb-2">Expected Response Text</label>
            <input
              id="udp-expect"
              type="text"
              value={formData.udpExpectContains}
              onChange={(e) => setFormData({ ...formData, udpExpectContains: e.target.value })}
              placeholder="Optional response fragment"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'dns' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label htmlFor="dns-record-type" className="block text-sm font-medium text-gray-300 mb-2">DNS Record Type</label>
            <select
              id="dns-record-type"
              value={formData.dnsRecordType}
              onChange={(e) => setFormData({ ...formData, dnsRecordType: e.target.value })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            >
              {['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SOA', 'PTR', 'SRV'].map((recordType) => (
                <option key={recordType} value={recordType}>{recordType}</option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="dns-expected" className="block text-sm font-medium text-gray-300 mb-2">Expected DNS Values</label>
            <input
              id="dns-expected"
              type="text"
              value={formData.dnsExpected}
              onChange={(e) => setFormData({ ...formData, dnsExpected: e.target.value })}
              placeholder="Optional, comma separated"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'icmp' && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div>
            <label htmlFor="icmp-count" className="block text-sm font-medium text-gray-300 mb-2">ICMP Count</label>
            <input
              id="icmp-count"
              type="number"
              min="1"
              value={formData.icmpCount}
              onChange={(e) => setFormData({ ...formData, icmpCount: parseInt(e.target.value) })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label htmlFor="icmp-interval" className="block text-sm font-medium text-gray-300 mb-2">Interval Seconds</label>
            <input
              id="icmp-interval"
              type="number"
              min="1"
              value={formData.icmpInterval}
              onChange={(e) => setFormData({ ...formData, icmpInterval: parseInt(e.target.value) })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label htmlFor="icmp-loss" className="block text-sm font-medium text-gray-300 mb-2">Max Loss Percent</label>
            <input
              id="icmp-loss"
              type="number"
              min="0"
              max="100"
              value={formData.icmpMaxLossPercent}
              onChange={(e) => setFormData({ ...formData, icmpMaxLossPercent: parseInt(e.target.value) })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'smtp' && (
        <div className="space-y-4">
          <label className="flex items-center gap-3 text-sm text-gray-300">
            <input
              type="checkbox"
              checked={formData.smtpStartTLS}
              onChange={(e) => setFormData({ ...formData, smtpStartTLS: e.target.checked })}
              className="w-4 h-4 rounded border-gray-600 text-amber-500 focus:ring-amber-500/20"
            />
            Require STARTTLS
          </label>
          <div>
            <label htmlFor="smtp-banner" className="block text-sm font-medium text-gray-300 mb-2">Expected SMTP Banner</label>
            <input
              id="smtp-banner"
              type="text"
              value={formData.smtpBannerContains}
              onChange={(e) => setFormData({ ...formData, smtpBannerContains: e.target.value })}
              placeholder="Optional banner fragment"
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}

      {formData.type === 'grpc' && (
        <div>
          <label htmlFor="grpc-service" className="block text-sm font-medium text-gray-300 mb-2">gRPC Service Name</label>
          <input
            id="grpc-service"
            type="text"
            value={formData.grpcService}
            onChange={(e) => setFormData({ ...formData, grpcService: e.target.value })}
            placeholder="Optional health service"
            className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
          />
        </div>
      )}

      {formData.type === 'websocket' && (
        <div className="space-y-4">
          <label className="flex items-center gap-3 text-sm text-gray-300">
            <input
              type="checkbox"
              checked={formData.websocketPingCheck}
              onChange={(e) => setFormData({ ...formData, websocketPingCheck: e.target.checked })}
              className="w-4 h-4 rounded border-gray-600 text-amber-500 focus:ring-amber-500/20"
            />
            Send WebSocket ping
          </label>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label htmlFor="websocket-send" className="block text-sm font-medium text-gray-300 mb-2">Send Message</label>
              <input
                id="websocket-send"
                type="text"
                value={formData.websocketSend}
                onChange={(e) => setFormData({ ...formData, websocketSend: e.target.value })}
                placeholder="Optional message"
                className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
              />
            </div>
            <div>
              <label htmlFor="websocket-expect" className="block text-sm font-medium text-gray-300 mb-2">Expected Message Text</label>
              <input
                id="websocket-expect"
                type="text"
                value={formData.websocketExpectContains}
                onChange={(e) => setFormData({ ...formData, websocketExpectContains: e.target.value })}
                placeholder="Optional response fragment"
                className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder:text-gray-600 focus:outline-none focus:border-amber-500/50"
              />
            </div>
          </div>
        </div>
      )}

      {formData.type === 'tls' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label htmlFor="tls-warn" className="block text-sm font-medium text-gray-300 mb-2">Expiry Warning Days</label>
            <input
              id="tls-warn"
              type="number"
              min="1"
              value={formData.tlsExpiryWarnDays}
              onChange={(e) => setFormData({ ...formData, tlsExpiryWarnDays: parseInt(e.target.value) })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
          <div>
            <label htmlFor="tls-critical" className="block text-sm font-medium text-gray-300 mb-2">Expiry Critical Days</label>
            <input
              id="tls-critical"
              type="number"
              min="1"
              value={formData.tlsExpiryCriticalDays}
              onChange={(e) => setFormData({ ...formData, tlsExpiryCriticalDays: parseInt(e.target.value) })}
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white focus:outline-none focus:border-amber-500/50"
            />
          </div>
        </div>
      )}
    </div>
  )
}
