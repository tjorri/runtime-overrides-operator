{{/*
SPDX-License-Identifier: Apache-2.0
Common helpers for runtime-overrides-operator chart templates.
*/}}

{{- define "operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{- define "operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "operator.namespace" -}}
{{- .Release.Namespace -}}
{{- end -}}

{{- define "operator.serviceAccountName" -}}
{{- include "operator.fullname" . -}}
{{- end -}}

{{- define "operator.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: runtime-overrides-operator
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "operator.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/* Certificate / Issuer names used by the webhook + cert-manager wiring. */}}
{{- define "operator.webhookCertName" -}}
{{- printf "%s-webhook-cert" (include "operator.fullname" .) -}}
{{- end -}}

{{- define "operator.webhookIssuerName" -}}
{{- printf "%s-selfsigned" (include "operator.fullname" .) -}}
{{- end -}}

{{- define "operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "operator.fullname" .) -}}
{{- end -}}
