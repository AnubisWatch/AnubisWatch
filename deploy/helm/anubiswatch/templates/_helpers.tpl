{{/*
Expand the name of the chart.
*/}}
{{- define "anubiswatch.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "anubiswatch.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "anubiswatch.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "anubiswatch.labels" -}}
helm.sh/chart: {{ include "anubiswatch.chart" . }}
{{ include "anubiswatch.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "anubiswatch.selectorLabels" -}}
app.kubernetes.io/name: {{ include "anubiswatch.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "anubiswatch.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "anubiswatch.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Config file name
*/}}
{{- define "anubiswatch.configName" -}}
{{ include "anubiswatch.fullname" . }}-config
{{- end }}

{{/* Prefer a registry digest when supplied; explicit tags remain useful for local images. */}}
{{- define "anubiswatch.image" -}}
{{- if .Values.image.digest -}}
{{- if not (regexMatch "^sha256:[a-f0-9]{64}$" .Values.image.digest) -}}
{{- fail "image.digest must be a sha256 digest" -}}
{{- end -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}
{{- end -}}

{{/*
Name of the Secret holding admin-password, encryption-key and cluster-secret:
secrets.existingSecret when set, else the chart-managed Secret.
*/}}
{{- define "anubiswatch.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "anubiswatch.fullname" . }}-secrets
{{- end -}}
{{- end }}

{{/*
Whether the pods read encryption-key / cluster-secret. With existingSecret the
values are unknown to Helm, so the feature flags decide; the secretKeyRef is
not optional, so a missing key stops the pod rather than starting it unkeyed.
*/}}
{{- define "anubiswatch.hasEncryptionKey" -}}
{{- if or .Values.secrets.encryptionKey (and .Values.secrets.existingSecret .Values.config.storage.encryption.enabled) -}}true{{- end -}}
{{- end }}

{{- define "anubiswatch.hasMailPassword" -}}
{{- if and .Values.config.mail.host (or .Values.secrets.mailPassword .Values.secrets.existingSecret) -}}true{{- end -}}
{{- end }}

{{- define "anubiswatch.hasClusterSecret" -}}
{{- if or .Values.secrets.clusterSecret (and .Values.secrets.existingSecret .Values.config.necropolis.enabled) -}}true{{- end -}}
{{- end }}
