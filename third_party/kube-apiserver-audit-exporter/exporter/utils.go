package exporter

import (
	"strings"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

type target struct {
	Name      string
	Namespace string
}

func buildTarget(ref *auditv1.ObjectReference) target {
	if ref == nil || ref.Name == "" {
		return target{}
	}

	return target{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}
}

func extractUserAgent(ua string) string {
	parts := strings.SplitN(ua, "/", 2)
	name := strings.SplitN(parts[0], " ", 2)[0]
	if name == "" {
		name = "unknown"
	}
	return name
}

func extractResourceName(event auditv1.Event) string {
	ref := event.ObjectRef
	if ref == nil {
		return "None"
	}

	var builder strings.Builder
	builder.WriteString(ref.Resource)

	if ref.APIGroup != "" {
		builder.WriteString(".")
		builder.WriteString(ref.APIGroup)
	}
	if ref.Subresource != "" {
		builder.WriteString("/")
		builder.WriteString(ref.Subresource)
	}
	return builder.String()
}
