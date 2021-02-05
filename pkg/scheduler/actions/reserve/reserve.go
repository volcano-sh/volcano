package reserve

import (
	"k8s.io/klog"

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
	return "reserve"
}

// Initialize inits the action
func (alloc *Action) Initialize() {}

// Execute selects a node which is not locked and has the most idle resource
func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Reserve ...")
	defer klog.V(3).Infof("Leaving Reserve ...")
	if util.Reservation.TargetJob == nil {
		klog.V(4).Infof("No target job, skip reserve action")
		return
	}
	// if target job has not been scheduled, select a locked node for it
	// else reset target job and locked nodes
	util.Reservation.TargetJob = ssn.Jobs[util.Reservation.TargetJob.UID]

	if !util.Reservation.TargetJob.Ready() {
		ssn.ReservedNodes()
	} else {
		klog.V(3).Infof("Target Job has been scheduled. Reset target job")
		util.Reservation.TargetJob = nil
		for node := range util.Reservation.LockedNodes {
			delete(util.Reservation.LockedNodes, node)
		}
	}
}

// UnInitialize releases resource which are not useful.
func (alloc *Action) UnInitialize() {}
