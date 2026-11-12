{{- define "scalepilot.name" -}}scalepilot{{- end }}
{{- define "scalepilot.watchNamespace" -}}{{ .Values.watchNamespace | default .Release.Namespace }}{{- end }}
{{- define "scalepilot.labels" -}}
app.kubernetes.io/name: scalepilot
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
