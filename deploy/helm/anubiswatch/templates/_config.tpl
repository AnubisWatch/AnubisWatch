{{/* Runtime configuration shared by standalone and per-ordinal cluster files. */}}
{{- define "anubiswatch.runtimeConfig" -}}
{{- $root := .root -}}
{{- $clustered := $root.Values.config.necropolis.enabled -}}
{{- $fullname := include "anubiswatch.fullname" $root -}}
{{- $domain := $root.Values.cluster.domain | trimSuffix "." -}}
environment: {{ $root.Values.config.environment | quote }}
server:
  host: {{ $root.Values.config.server.host | quote }}
  port: {{ $root.Values.config.server.port }}
  metrics_auth: {{ $root.Values.config.server.metricsAuth }}
  grpc_port: {{ $root.Values.service.grpcPort }}
  tls:
    enabled: {{ $root.Values.config.server.tls.enabled }}
    {{- if $root.Values.config.server.tls.cert }}
    cert: {{ $root.Values.config.server.tls.cert | quote }}
    {{- end }}
    {{- if $root.Values.config.server.tls.key }}
    key: {{ $root.Values.config.server.tls.key | quote }}
    {{- end }}
    min_version: {{ $root.Values.config.server.tls.min_version | default 3 }}
  trusted_proxies: {{ $root.Values.config.server.trustedProxies | toJson }}
  {{- if $root.Values.config.server.publicURL }}
  public_url: {{ $root.Values.config.server.publicURL | quote }}
  {{- end }}
  allowed_origins: {{ $root.Values.config.server.allowedOrigins | toJson }}

auth:
  enabled: true
  type: {{ $root.Values.config.auth.type | quote }}
  local:
    admin_email: {{ $root.Values.config.auth.local.adminEmail | quote }}

dashboard:
  enabled: {{ $root.Values.config.dashboard.enabled }}

storage:
  path: {{ $root.Values.config.storage.path | quote }}
  encryption:
    enabled: {{ $root.Values.config.storage.encryption.enabled }}

necropolis:
  enabled: {{ $clustered }}
  {{- if $clustered }}
  {{- $nodeID := printf "%s-%d" $fullname (int .ordinal) }}
  {{- $address := printf "%s.%s-headless.%s.svc.%s:%v" $nodeID $fullname $root.Release.Namespace $domain $root.Values.service.clusterPort }}
  node_name: {{ $nodeID | quote }}
  bind_addr: {{ printf "0.0.0.0:%v" $root.Values.service.clusterPort | quote }}
  advertise_addr: {{ $address | quote }}
  cluster_secret: "${ANUBIS_CLUSTER_SECRET}"
  discovery:
    mode: manual
  raft:
    node_id: {{ $nodeID | quote }}
    bind_addr: {{ printf "0.0.0.0:%v" $root.Values.service.clusterPort | quote }}
    advertise_addr: {{ $address | quote }}
    bootstrap: false
    tls:
      cert_file: /etc/anubis/raft-tls/tls.crt
      key_file: /etc/anubis/raft-tls/tls.key
      ca_file: /etc/anubis/raft-tls/ca.crt
      verify_peers: true
      require_client_cert: true
    peers:
      {{- range $peer := until 3 }}
      {{- if ne $peer (int $.ordinal) }}
      - id: {{ printf "%s-%d" $fullname $peer | quote }}
        address: {{ printf "%s-%d.%s-headless.%s.svc.%s:%v" $fullname $peer $fullname $root.Release.Namespace $domain $root.Values.service.clusterPort | quote }}
        role: voter
      {{- end }}
      {{- end }}
  {{- end }}

{{- with $root.Values.config.mail }}
{{- if .host }}

mail:
  host: {{ .host | quote }}
  port: {{ .port | default 587 }}
  {{- if .username }}
  username: {{ .username | quote }}
  password: "${ANUBIS_MAIL_PASSWORD}"
  {{- end }}
  from: {{ required "config.mail.from is required when config.mail.host is set" .from | quote }}
  tls: {{ .tls | default "starttls" | quote }}
  timeout: {{ .timeout | default "30s" | quote }}
{{- end }}
{{- end }}

logging:
  level: {{ $root.Values.config.logging.level | quote }}
  format: {{ $root.Values.config.logging.format | quote }}
  output: stdout
{{- end -}}

{{/* Fixed membership and authenticated transport are prerequisites, not defaults to infer. */}}
{{- define "anubiswatch.clusterValidation" -}}
{{- if .Release.IsUpgrade -}}
{{- $fullname := include "anubiswatch.fullname" . -}}
{{- if and .Values.config.necropolis.enabled (lookup "apps/v1" "Deployment" .Release.Namespace $fullname) -}}
{{- fail "cannot change an existing standalone release into a cluster in place; use a separate release and a verified backup/restore migration" -}}
{{- end -}}
{{- if and (not .Values.config.necropolis.enabled) (lookup "apps/v1" "StatefulSet" .Release.Namespace $fullname) -}}
{{- fail "cannot change an existing cluster release into standalone in place; use a separate release and a verified backup/restore migration" -}}
{{- end -}}
{{- end -}}

{{- if .Values.config.necropolis.enabled -}}
{{- if and (not .Values.image.digest) (or (not (.Values.image.tag | trim)) (eq (.Values.image.tag | trim | lower) "latest")) -}}
{{- fail "cluster mode requires an explicit image.tag other than latest or an image.digest; the default appVersion does not imply HA authentication support" -}}
{{- end -}}
{{- if gt (len (include "anubiswatch.fullname" .)) 54 -}}
{{- fail "cluster release fullname must be at most 54 characters so the headless Service remains a valid DNS label" -}}
{{- end -}}
{{- if ne .Values.config.auth.type "local" -}}
{{- fail "config.auth.type must be local for the validated cluster authentication path" -}}
{{- end -}}
{{- if ne (int .Values.statefulSet.replicas) 3 -}}
{{- fail "statefulSet.replicas must be 3 for the fixed three-voter cluster; this chart does not support arbitrary membership scaling" -}}
{{- end -}}
{{- if not .Values.persistence.enabled -}}
{{- fail "persistence.enabled must be true for cluster mode; each voter requires its own durable PVC" -}}
{{- end -}}
{{- if .Values.autoscaling.enabled -}}
{{- fail "autoscaling.enabled must be false for the fixed three-voter cluster" -}}
{{- end -}}
{{- if ne .Values.statefulSet.podManagementPolicy "Parallel" -}}
{{- fail "statefulSet.podManagementPolicy must be Parallel so the initial voters can form a quorum" -}}
{{- end -}}
{{- if not (.Values.cluster.raftTLSSecret | trim) -}}
{{- fail "cluster.raftTLSSecret is required in cluster mode and must contain tls.crt, tls.key, and ca.crt for verified peer mTLS" -}}
{{- end -}}
{{- if not (or (.Values.secrets.clusterSecret | trim) .Values.secrets.existingSecret) -}}
{{- fail "secrets.clusterSecret (or secrets.existingSecret with a cluster-secret key) is required in cluster mode" -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$" (.Values.cluster.domain | trimSuffix ".")) -}}
{{- fail "cluster.domain must be a DNS suffix, for example cluster.local" -}}
{{- end -}}
{{- end -}}
{{- end -}}
