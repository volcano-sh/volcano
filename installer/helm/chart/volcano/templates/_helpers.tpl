{{/*
Define apiVersion for CRD.
bases stands for v1.
*/}}
{{- define "crd_version" -}} 
{{- if .Capabilities.APIVersions.Has "apiextensions.k8s.io/v1" -}}
bases
{{- else -}}
{{- fail "Volcano requires the apiextensions.k8s.io/v1 CustomResourceDefinition API; the deprecated v1beta1 CRD installation path is not supported" -}}
{{- end -}}
{{- end -}}

{{/* Validate and return the HyperNode controller deployment mode. */}}
{{- define "hypernodeControllerMode" -}}
{{- $mode := .Values.custom.hypernode_controller_mode | default "controller-manager" -}}
{{- if not (has $mode (list "controller-manager" "standalone" "disabled")) -}}
{{- fail (printf "custom.hypernode_controller_mode must be one of controller-manager, standalone, or disabled; got %q" $mode) -}}
{{- end -}}
{{- if and (eq $mode "standalone") (lt (int .Values.custom.hypernode_controller_replicas) 1) -}}
{{- fail "custom.hypernode_controller_replicas must be at least 1 in standalone mode; use disabled mode to stop HyperNode reconciliation" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}
