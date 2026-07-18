{{- define "gophprofile.name" -}}
{{- default .Chart.Name .Values.nameOverride }}
{{- end }}

{{- define "gophprofile.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name }}
{{- end }}
{{- end }}
{{- end }}

{{- define "gophprofile.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end }}

{{- define "gophprofile.labels" -}}
helm.sh/chart: {{ include "gophprofile.chart" . }}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "gophprofile.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gophprofile.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "gophprofile.configMapName" -}}
{{- if .Values.config.create }}
{{- printf "%s-config" (include "gophprofile.fullname" .) }}
{{- else }}
{{- required "config.existingConfigMap is required when config.create=false" .Values.config.existingConfigMap }}
{{- end }}
{{- end }}

{{- define "gophprofile.secretName" -}}
{{- if .Values.secret.create }}
{{- printf "%s-secret" (include "gophprofile.fullname" .) }}
{{- else }}
{{- required "secret.existingSecret is required when secret.create=false" .Values.secret.existingSecret }}
{{- end }}
{{- end }}

{{- define "gophprofile.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}
