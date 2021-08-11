// Package elect is used to find the target job and reserve resource for it
package elect

import (
	"k8s.io/klog"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// Action defines the action
type Action struct{}

// New returns the action instance
func New() *Action {
	return &Action{}
}

// Name returns the action name
func (alloc *Action) Name() string {
	return "elect"
}

// Initialize inits the action
func (alloc *Action) Initialize() {}

// Execute selects the target job which is of the highest priority and waits for the longest time.
func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Elect ...")
	defer klog.V(3).Infof("Leaving Elect ...")

	if util.Reservation.TargetJob == nil {
		klog.V(4).Infof("Start select Target Job")
		var pendingJobs []*api.JobInfo
		for _, job := range ssn.Jobs {
			if job.PodGroup.Status.Phase == scheduling.PodGroupPending {
				pendingJobs = append(pendingJobs, job)
			}
		}
		util.Reservation.TargetJob = ssn.TargetJob(pendingJobs)
		if util.Reservation.TargetJob != nil {
			klog.V(3).Infof("Target Job name: %s", util.Reservation.TargetJob.Name)
		} else {
			klog.V(3).Infof("Target Job name: nil")
		}
	}
}

// UnInitialize releases resource which are not useful.
func (alloc *Action) UnInitialize() {}
