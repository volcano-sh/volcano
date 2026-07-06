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

{{/* Validate and return the NamespaceQueue hierarchy depth limit. */}}
{{- define "volcano.namespaceQueueMaxDepth" -}}
{{- $depth := int .Values.custom.namespace_queue_max_depth -}}
{{- if lt $depth 1 -}}
{{- fail "custom.namespace_queue_max_depth must be greater than zero" -}}
{{- end -}}
{{- $depth -}}
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

{{/*
Merge the NamespaceQueue feature gate into a component's existing gates.
The chart-level switch takes precedence over an explicitly configured
NamespaceQueue value while preserving all unrelated gates.
*/}}
{{- define "volcano.featureGates" -}}
{{- $configured := .gates | default "" -}}
{{- if .namespaceQueueEnabled -}}
{{- $merged := list -}}
{{- range $gate := splitList "," $configured -}}
{{- $gate = trim $gate -}}
{{- if and $gate (not (hasPrefix "NamespaceQueue=" $gate)) -}}
{{- $merged = append $merged $gate -}}
{{- end -}}
{{- end -}}
{{- $merged = append $merged "NamespaceQueue=true" -}}
{{- join "," $merged -}}
{{- else -}}
{{- $configured -}}
{{- end -}}
{{- end -}}
