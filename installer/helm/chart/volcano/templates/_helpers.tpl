{{/*
A reusable template to generate a Kubernetes liveness or readiness probe configuration.
Context MUST be a dict with:
  "values" - the custom probe values for that component/probe (may be nil/empty)
  "scheme" - HTTP or HTTPS
  "port"   - the healthz port to probe
Example call:
{{ include "volcano.probe" (dict "values" .Values.custom.admission_liveness_probe "scheme" "HTTPS" "port" .Values.basic.healthz_port) }}
*/}}
{{- define "volcano.probe" -}}
{{- $values := .values | default dict -}}
httpGet:
  path: /healthz
  port: {{ .port }}
  scheme: {{ .scheme }}
initialDelaySeconds: {{ $values.initialDelaySeconds | default 10 }}
periodSeconds: {{ $values.periodSeconds | default 20 }}
timeoutSeconds: {{ $values.timeoutSeconds | default 5 }}
failureThreshold: {{ $values.failureThreshold | default 3 }}
successThreshold: {{ $values.successThreshold | default 1 }}
{{- end -}}
