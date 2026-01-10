package v1alpha1

// Action is the action that JobFlow controller will take according to the event.
type Action string

const (
	// SyncJobFlowAction is the action to sync JobFlow status.
	SyncJobFlowAction Action = "SyncJobFlow"
	// SyncJobTemplateAction is the action to sync JobTemplate status.
	SyncJobTemplateAction Action = "SyncJobTemplate"
)
