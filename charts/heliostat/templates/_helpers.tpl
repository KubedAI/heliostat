{{- define "heliostat.name" -}}heliostat{{- end -}}

{{- define "heliostat.fullname" -}}
{{- printf "%s-heliostat" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "heliostat.selectorLabels" -}}
app.kubernetes.io/name: {{ include "heliostat.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "heliostat.labels" -}}
{{ include "heliostat.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "heliostat.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "heliostat.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "heliostat.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ default .Chart.AppVersion .Values.image.tag }}
{{- end -}}
{{- end -}}

{{- define "heliostat.config" -}}
clusters:
{{- toYaml .Values.clusters | nindent 2 }}
collector:
{{- toYaml .Values.collector | nindent 2 }}
retention:
{{- toYaml .Values.retention | nindent 2 }}
{{- end -}}

{{/* Fails the render when a LoadBalancer would expose the unauthenticated UI to the internet. */}}
{{- define "heliostat.validateService" -}}
{{- if and (eq .Values.service.type "LoadBalancer") (not .Values.service.allowInternetFacing) -}}
{{- $annotations := .Values.service.annotations | default dict -}}
{{- $scheme := index $annotations "service.beta.kubernetes.io/aws-load-balancer-scheme" | default "" -}}
{{- $legacy := index $annotations "service.beta.kubernetes.io/aws-load-balancer-internal" | default "" | toString -}}
{{- if not (or (eq $scheme "internal") (eq $legacy "true")) -}}
{{- fail "service.type=LoadBalancer must be internal: set annotation service.beta.kubernetes.io/aws-load-balancer-scheme: internal (Heliostat has no authentication and proxies Ray logs), or set service.allowInternetFacing=true to override." -}}
{{- end -}}
{{- end -}}
{{- end -}}
