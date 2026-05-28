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
Controller gates for vc-controller-manager.
When hypernode standalone is enabled and controller_enabled_controllers is unset,
exclude hyperNode-controller to avoid duplicate reconciliation with vc-hypernode-controller.
*/}}
{{- define "volcano.controllerGates" -}}
{{- if .Values.custom.controller_enabled_controllers -}}
{{- .Values.custom.controller_enabled_controllers -}}
{{- else if .Values.custom.hypernode_controller_standalone_enable -}}
*,-sharding-controller,-hyperNode-controller
{{- end -}}
{{- end -}}