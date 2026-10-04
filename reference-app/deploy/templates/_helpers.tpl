{{- define "foundation-reference.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: foundation-reference
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "foundation-reference.selectorLabels" -}}
app.kubernetes.io/name: foundation-reference
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
