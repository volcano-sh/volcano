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
Example Call: 
{{ include "volcano.probe" (dict "values" .Values.admission_liveness "scheme" "HTTPS" "port" 8080) }}
*/}}
{{- define "volcano.probe" -}}
httpGet:
  path: /healthz
  port: {{ .port }}
  scheme: {{ .scheme }}
initialDelaySeconds: {{ .values.initialDelaySeconds | default 10 }}
periodSeconds: {{ .values.periodSeconds | default 20 }}
timeoutSeconds: {{ .values.timeoutSeconds | default 5 }}
failureThreshold: {{ .values.failureThreshold | default 3 }}
successThreshold: {{ .values.successThreshold | default 1 }}
{{- end -}}