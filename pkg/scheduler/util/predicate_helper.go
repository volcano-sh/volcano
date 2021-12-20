package util

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type PredicateHelper interface {
	PredicateNodes(task *api.TaskInfo, nodes []*api.NodeInfo, fn api.PredicateFn) ([]*api.NodeInfo, *api.FitErrors)
}

type predicateHelper struct {
	taskPredicateErrorCache map[string]map[string]error
}

// PredicateNodes returns the specified number of nodes that fit a task
func (ph *predicateHelper) PredicateNodes(task *api.TaskInfo, nodes []*api.NodeInfo, fn api.PredicateFn) ([]*api.NodeInfo, *api.FitErrors) {
	var errorLock sync.RWMutex
	fe := api.NewFitErrors()

	allNodes := len(nodes)
	if allNodes == 0 {
		return make([]*api.NodeInfo, 0), fe
	}
	numNodesToFind := CalculateNumOfFeasibleNodesToFind(int32(allNodes))

	//allocate enough space to avoid growing it
	predicateNodes := make([]*api.NodeInfo, numNodesToFind)

	numFoundNodes := int32(0)
	processedNodes := int32(0)

	taskGroupid := taskGroupID(task)
	nodeErrorCache, taskFailedBefore := ph.taskPredicateErrorCache[taskGroupid]
	if nodeErrorCache == nil {
		nodeErrorCache = map[string]error{}
	}

	//create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	checkNode := func(index int) {
		// Check the nodes starting from where is left off in the previous scheduling cycle,
		// to make sure all nodes have the same chance of being examined across pods.
		node := nodes[(lastProcessedNodeIndex+index)%allNodes]
		atomic.AddInt32(&processedNodes, 1)
		klog.V(4).Infof("Considering Task <%v/%v> on node <%v>: <%v> vs. <%v>",
			task.Namespace, task.Name, node.Name, task.Resreq, node.Idle)

		// Check if the task had "predicate" failure before.
		// And then check if the task failed to predict on this node before.
		if taskFailedBefore {
			errorLock.RLock()
			errC, ok := nodeErrorCache[node.Name]
			errorLock.RUnlock()

			if ok {
				errorLock.Lock()
				fe.SetNodeError(node.Name, errC)
				errorLock.Unlock()
				return
			}
		}

		// TODO (k82cn): Enable eCache for performance improvement.
		if err := fn(task, node); err != nil {
			klog.V(3).Infof("Predicates failed for task <%s/%s> on node <%s>: %v",
				task.Namespace, task.Name, node.Name, err)
			errorLock.Lock()
			nodeErrorCache[node.Name] = err
			ph.taskPredicateErrorCache[taskGroupid] = nodeErrorCache
			fe.SetNodeError(node.Name, err)
			errorLock.Unlock()
			return
		}

		//check if the number of found nodes is more than the numNodesTofind
		length := atomic.AddInt32(&numFoundNodes, 1)
		if length > numNodesToFind {
			cancel()
			atomic.AddInt32(&numFoundNodes, -1)
		} else {
			predicateNodes[length-1] = node
		}
	}

	//workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), checkNode)
	workqueue.ParallelizeUntil(ctx, 16, allNodes, checkNode)

	//processedNodes := int(numFoundNodes) + len(filteredNodesStatuses) + len(failedPredicateMap)
	lastProcessedNodeIndex = (lastProcessedNodeIndex + int(processedNodes)) % allNodes
	predicateNodes = predicateNodes[:numFoundNodes]
	return predicateNodes, fe
}

func taskGroupID(task *api.TaskInfo) string {
	return fmt.Sprintf("%s/%s", task.Job, task.GetTaskSpecKey())
}

func NewPredicateHelper() PredicateHelper {
	return &predicateHelper{taskPredicateErrorCache: map[string]map[string]error{}}
}
