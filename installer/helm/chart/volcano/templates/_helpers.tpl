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
