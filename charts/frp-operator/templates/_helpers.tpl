{{/*
Chart name (truncated to 63 chars per k8s).
*/}}
{{- define "frp-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. Honors fullnameOverride, then release name + chart name.
*/}}
{{- define "frp-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name + version label.
*/}}
{{- define "frp-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels.
*/}}
{{- define "frp-operator.labels" -}}
helm.sh/chart: {{ include "frp-operator.chart" . }}
{{ include "frp-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "frp-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "frp-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name.
*/}}
{{- define "frp-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "frp-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Leader election lease namespace.
*/}}
{{- define "frp-operator.leaseNamespace" -}}
{{- default .Release.Namespace .Values.leaderElection.leaseNamespace -}}
{{- end -}}
