{{- /*
Copyright The Linux Foundation and each contributor to LFX.
SPDX-License-Identifier: MIT
*/ -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "lfx-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "lfx-mcp.fullname" -}}
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
{{- define "lfx-mcp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "lfx-mcp.labels" -}}
helm.sh/chart: {{ include "lfx-mcp.chart" . }}
{{ include "lfx-mcp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "lfx-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "lfx-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Derive the public MCP URL from the ingress hostname.
*/}}
{{- define "lfx-mcp.mcpPublicURL" -}}
{{- $hostname := .Values.ingress.hostname | trim }}
{{- if $hostname }}
{{- printf "https://%s/mcp" $hostname }}
{{- end }}
{{- end }}
