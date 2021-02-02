package validate

import (
	"strconv"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

// Arguments map
type Arguments map[string]string

// GetArgOfActionFromConf return argument of action reading from configuration of schedule
func GetArgOfActionFromConf(configurations []conf.Configuration, actionName string) Arguments {
	for _, c := range configurations {
		if c.Name == actionName {
			return c.Arguments
		}
	}

	return nil
}

// GetFloat64 get the float64 value from string
func (a Arguments) GetFloat64(ptr *float64, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok || len(argv) == 0 {
		return
	}

	value, err := strconv.ParseFloat(argv, 64)
	if err != nil {
		klog.Warningf("Could not parse argument: %s for key %s, with err %v", argv, key, err)
		return
	}

	*ptr = value
}
