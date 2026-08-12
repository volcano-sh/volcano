/*
Copyright 2026 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"fmt"
	"net/url"
	"strconv"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const rootQueueID api.QueueID = "root"

type queueVisitState uint8

const (
	queueUnvisited queueVisitState = iota
	queueVisiting
	queueVisited
)

// BuildEffectiveQueueHierarchy fills the session-local hierarchy fields used
// by hierarchical plugins. Existing cluster Queue annotations are preserved;
// NamespaceQueues are derived from canonical parent IDs.
func BuildEffectiveQueueHierarchy(queues map[api.QueueID]*api.QueueInfo) error {
	states := make(map[api.QueueID]queueVisitState, len(queues))
	hierarchies := make(map[api.QueueID]string, len(queues))
	weights := make(map[api.QueueID]string, len(queues))

	var build func(api.QueueID) (string, string, error)
	build = func(queueID api.QueueID) (string, string, error) {
		queue, ok := queues[queueID]
		if !ok || queue == nil {
			return "", "", fmt.Errorf("queue %q is not present", queueID)
		}

		if queue.Scope == api.ClusterQueueScope &&
			queue.Hierarchy != "" && queue.Weights != "" {
			states[queueID] = queueVisited
			hierarchies[queueID] = queue.Hierarchy
			weights[queueID] = queue.Weights
			return queue.Hierarchy, queue.Weights, nil
		}

		switch states[queueID] {
		case queueVisiting:
			return "", "", fmt.Errorf("queue hierarchy contains a cycle at %q", queueID)
		case queueVisited:
			return hierarchies[queueID], weights[queueID], nil
		}

		states[queueID] = queueVisiting
		if queueID == rootQueueID {
			states[queueID] = queueVisited
			hierarchies[queueID] = "root"
			weights[queueID] = "1"
			return hierarchies[queueID], weights[queueID], nil
		}

		parentID := queue.Parent
		if parentID == "" {
			parentID = rootQueueID
		}
		parentHierarchy, parentWeights, err := build(parentID)
		if err != nil {
			return "", "", fmt.Errorf("build hierarchy for queue %q: %w", queueID, err)
		}

		queueHierarchy := parentHierarchy + "/" + hierarchySegment(queue)
		queueWeight := queue.Weight
		if queueWeight < 1 {
			queueWeight = api.DefaultQueueWeight
		}
		queueWeights := parentWeights + "/" + strconv.FormatInt(int64(queueWeight), 10)

		states[queueID] = queueVisited
		hierarchies[queueID] = queueHierarchy
		weights[queueID] = queueWeights
		queue.Hierarchy = queueHierarchy
		queue.Weights = queueWeights

		return queueHierarchy, queueWeights, nil
	}

	for queueID := range queues {
		if _, _, err := build(queueID); err != nil {
			return err
		}
	}

	return nil
}

func hierarchySegment(queue *api.QueueInfo) string {
	if queue.Scope == api.NamespaceQueueScope {
		return url.PathEscape(string(queue.UID))
	}
	return queue.Name
}
