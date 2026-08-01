{{/*
Expand the name of the chart.
*/}}
{{- define "aura-power.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "aura-power.fullname" -}}
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
{{- define "aura-power.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aura-power.labels" -}}
helm.sh/chart: {{ include "aura-power.chart" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Server labels
*/}}
{{- define "aura-power.server.labels" -}}
{{ include "aura-power.labels" . }}
{{ include "aura-power.server.selectorLabels" . }}
{{- end }}

{{/*
Server selector labels
*/}}
{{- define "aura-power.server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aura-power.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Controller labels
*/}}
{{- define "aura-power.controller.labels" -}}
{{ include "aura-power.labels" . }}
{{ include "aura-power.controller.selectorLabels" . }}
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "aura-power.controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aura-power.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Server full name
*/}}
{{- define "aura-power.server.fullname" -}}
{{- printf "%s-server" (include "aura-power.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Controller full name
*/}}
{{- define "aura-power.controller.fullname" -}}
{{- printf "%s-controller" (include "aura-power.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Server service account name
*/}}
{{- define "aura-power.server.serviceAccountName" -}}
{{- if .Values.server.serviceAccount.create }}
{{- default (include "aura-power.server.fullname" .) .Values.server.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.server.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Controller service account name
*/}}
{{- define "aura-power.controller.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.create }}
{{- default (include "aura-power.controller.fullname" .) .Values.controller.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.controller.serviceAccount.name }}
{{- end }}
{{- end }}
