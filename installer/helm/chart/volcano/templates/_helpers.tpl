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
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "volcano.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create a default fully qualified admission name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "volcano.admission.fullname" -}}
{{- printf "%s-%s" (include "volcano.fullname" .) "admission" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified controllers name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "volcano.controllers.fullname" -}}
{{- printf "%s-%s" (include "volcano.fullname" .) "controllers" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified scheduler name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "volcano.scheduler.fullname" -}}
{{- printf "%s-%s" (include "volcano.fullname" .) "scheduler" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified agent name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "volcano.agent.fullname" -}}
{{- printf "%s-%s" (include "volcano.fullname" .) "agent" | trunc 63 | trimSuffix "-" -}}
{{- end -}}
