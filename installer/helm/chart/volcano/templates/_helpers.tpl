{{/*
Define apiVersion for CRD.
bases stands for v1.
*/}}
{{- define "crd_version" -}}
{{- if .Capabilities.APIVersions.Has "apiextensions.k8s.io/v1" -}}
bases
{{- else -}}
v1beta1
{{- end -}}
{{- end -}}

{{/*
A reusable template to generate a Kubernetes Liveness or Readiness probe configuration.
Context MUST be the custom probe values for that specific component/probe.
Example: (dict "values" .Values.custom.admission_livenessProbe "scheme" "HTTPS")
*/}}
{{- define "volcano.probe" -}}
{{- $ctx := . -}}
{{- $values := .values -}}
httpGet:
  path: /healthz
  port: {{ default (int .Values.basic.healthz_port) $ctx.port }}
  scheme: {{ default "HTTP" $ctx.scheme }}
initialDelaySeconds: {{ $values.initialDelaySeconds | default 10 }}
periodSeconds: {{ $values.periodSeconds | default 20 }}
timeoutSeconds: {{ $values.timeoutSeconds | default 5 }}
failureThreshold: {{ $values.failureThreshold | default 3 }}
successThreshold: {{ $values.successThreshold | default 1 }}
{{- end -}}