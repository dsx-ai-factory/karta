
{{/*
Fully qualified resource name: "<chart-name>-operator" (e.g. "karta-operator").
Override wholesale via fullnameOverride.
*/}}
{{- define "karta.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-operator" .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Common labels stamped onto every resource.
*/}}
{{- define "karta.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "karta.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels. These must be stable across upgrades, so they intentionally
exclude version/chart labels (which change between releases).
*/}}
{{- define "karta.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Name of the ServiceAccount to use. When create=true the chart owns the name
(karta.fullname). When create=false the caller must supply serviceAccount.name.
*/}}
{{- define "karta.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- include "karta.fullname" . -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Resolves and validates global.fipsMode, returning "off", "on", or "only". The
value lives under global rather than scoped to this chart, so an umbrella
chart embedding karta as a dependency can drive it with one flag alongside
its other components. An unquoted on/off/yes/no/true/false in values.yaml
parses as a YAML 1.1 boolean, so that case is coerced before validating.
*/}}
{{- define "karta.fipsMode" -}}
{{- $fipsMode := (.Values.global).fipsMode | default "off" -}}
{{- if kindIs "bool" $fipsMode -}}
  {{- $fipsMode = ternary "on" "off" $fipsMode -}}
{{- end -}}
{{- if not (has $fipsMode (list "off" "on" "only")) -}}
  {{- fail (printf "global.fipsMode must be one of: off, on, only (got %v)" $fipsMode) -}}
{{- end -}}
{{- $fipsMode -}}
{{- end -}}
