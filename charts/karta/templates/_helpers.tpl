
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
Name for the CRD upgrader hook resources. Includes the release namespace and
name so separate releases never share the cluster-scoped ClusterRole/Binding.
*/}}
{{- define "karta.crdUpgrader.name" -}}
{{- printf "%s-crd-upgrader-%s-%s" (include "karta.fullname" .) .Release.Namespace .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Webhook resource names. The service name doubles as the serving cert SAN, so it
must match the --webhook-service-name flag passed to the operator.
*/}}
{{- define "karta.webhook.serviceName" -}}
{{- printf "%s-webhook" (include "karta.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "karta.webhook.secretName" -}}
{{- printf "%s-webhook-cert" (include "karta.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "karta.webhook.configName" -}}
{{- printf "%s-mutating" (include "karta.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "karta.webhook.validatingConfigName" -}}
{{- printf "%s-validating" (include "karta.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Validates fipsMode. Included from both deployment.yaml and crd-upgrader-job.yaml,
since deployment.yaml renders nothing when operator.enabled=false (CRD-only
install) and crd-upgrader-job.yaml reads fipsMode independently of that flag.
%v, not %q: an unquoted on/off/yes/no/true/false in values.yaml parses as a YAML
1.1 boolean, and %q on a non-string prints "%!q(bool=true)" instead of the value.
*/}}
{{- define "karta.validateFipsMode" -}}
{{- if not (has .Values.fipsMode (list "off" "on" "only")) -}}
  {{- fail (printf "fipsMode must be a quoted string, one of: off, on, only (got %v)" .Values.fipsMode) -}}
{{- end -}}
{{- end -}}
