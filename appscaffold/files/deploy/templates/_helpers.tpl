{{- define "__APP_NAME__.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: __APP_NAME__
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "__APP_NAME__.selectorLabels" -}}
app.kubernetes.io/name: __APP_NAME__
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
