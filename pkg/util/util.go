package util

const (
	defaultSchedulerName = "volcano"
)

// Contains check if slice contains element
func Contains(slice []string, element string) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// GenerateComponentName generate component name volcano
func GenerateComponentName(schedulerNames []string) string {
	if len(schedulerNames) == 1 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}

// GenerateSchedulerName generate scheduler name for volcano job
func GenerateSchedulerName(schedulerNames []string) string {
	// choose the first scheduler name for volcano job if its schedulerName is empty
	if len(schedulerNames) > 0 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}
