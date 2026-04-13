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
A reusable template to generate a Kubernetes Readiness probe configuration.
Context MUST be the custom probe values for that specific component.
Example Call: 
{{- include "volcano.readiness.probe" (dict "port" ( .Values.basic.healthz_port | default 11251 ) "scheme" "HTTPS" "values" .Values.custom.admission_readinessProbe) }}
*/}}
{{- define "volcano.readiness.probe" -}}
httpGet:
  path: /healthz
  port: {{ .port }}
  scheme: {{ .scheme }}
initialDelaySeconds: {{ .values.initialDelaySeconds | default 10 }}
periodSeconds: {{ .values.periodSeconds | default 10 }}
timeoutSeconds: {{ .values.timeoutSeconds | default 5 }}
failureThreshold: {{ .values.failureThreshold | default 5 }}
successThreshold: {{ .values.successThreshold | default 1 }}
{{- end -}}

{{/*
A reusable template to generate a Kubernetes Liveness probe configuration.
Context MUST be the custom probe values for that specific component.
Example Call: 
{{- include "volcano.liveness.probe" (dict "port" ( .Values.basic.healthz_port | default 11251 ) "scheme" "HTTPS" "values" .Values.custom.admission_livenessProbe) }}
*/}}
{{- define "volcano.liveness.probe" -}}
httpGet:
  path: /healthz
  port: {{ .port }}
  scheme: {{ .scheme }}
initialDelaySeconds: {{ .values.initialDelaySeconds | default 30 }}
periodSeconds: {{ .values.periodSeconds | default 10 }}
timeoutSeconds: {{ .values.timeoutSeconds | default 5 }}
failureThreshold: {{ .values.failureThreshold | default 5 }}
successThreshold: {{ .values.successThreshold | default 1 }}
{{- end -}}