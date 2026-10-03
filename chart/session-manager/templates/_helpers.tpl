{{- define "session-manager.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "session-manager.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "session-manager.name" . -}}
{{- $legacyDeployment := lookup "apps/v1" "Deployment" .Release.Namespace $name -}}
{{- $legacyAnnotations := dig "metadata" "annotations" (dict) $legacyDeployment -}}
{{- if and $legacyDeployment (eq (get $legacyAnnotations "meta.helm.sh/release-name") .Release.Name) -}}
{{- $name -}}
{{- else if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end }}
{{- define "session-manager.labels" -}}
app.kubernetes.io/name: {{ include "session-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}
{{- define "session-manager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "session-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
{{- end }}
{{- define "session-manager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}{{ default (include "session-manager.fullname" .) .Values.serviceAccount.name }}{{ else }}{{ required "serviceAccount.name is required when create=false" .Values.serviceAccount.name }}{{ end }}
{{- end }}
{{- define "session-manager.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}
{{- define "session-manager.managerIdLabelValue" -}}
{{- regexReplaceAll "[^a-zA-Z0-9_.-]" .Values.runner.managerId "-" | trunc 63 | trimAll "-_." }}
{{- end }}
