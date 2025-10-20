package capacitycard

import (
	"math"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// NodeOrderFn scores nodes for tasks with multi-card type options.
// For tasks with multiple card type options (separated by "|"), nodes that can provide
// card types on the left (higher priority) get higher scores.
// Example: "NVIDIA-A100|NVIDIA-H100" means A100 has higher priority than H100.
//
// The scoring algorithm:
//
//	baseScore = maxScore * (decayRate ^ index)
//	finalScore = baseScore * nodeOrderWeight
//
// where:
//   - maxScore is 100
//   - decayRate is fixed at 0.5 (each subsequent card type gets 50% less base score)
//   - index is the position of the card type (0-based, 0 is highest priority)
//   - nodeOrderWeight is configurable (default: 1.0, must be positive)
//
// This ensures every card type has a unique, non-zero score, even with many options.
// The nodeOrderWeight allows adjusting the overall importance of card type priority.
func (p *Plugin) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
	// Get card name from task annotation
	if task.Pod == nil {
		return 0, nil
	}

	cardName := p.getCardNameFromTask(task)
	if cardName == "" {
		// No card requested, no ordering preference
		return 0, nil
	}

	// Check if this is a multi-card type task (contains separator "|")
	if !strings.Contains(cardName, MultiCardSeparator) {
		// Single card type, no ordering preference
		return 0, nil
	}

	// Split multi-card types, left has higher priority
	multiCardNames := strings.Split(cardName, MultiCardSeparator)
	if len(multiCardNames) <= 1 {
		return 0, nil
	}

	// Get node card information
	if node.Node == nil {
		return 0, nil
	}

	nodeCardInfo := p.getCardResourceFromNode(node.Node)
	if len(nodeCardInfo.CardResource) == 0 {
		return 0, nil
	}

	// Iterate through card types from left to right (high to low priority)
	// Assign score based on position using exponential decay
	// Note: NodeOrderFn is called after filtering, so the node already satisfies resource requirements
	score := float64(0)
	for idx, singleCardName := range multiCardNames {
		// Check if node has this card type
		_, ok := nodeCardInfo.CardNameToResourceName[v1.ResourceName(singleCardName)]
		if !ok {
			// Node doesn't have this card type, continue to next
			continue
		}

		// Node has this card type, calculate score based on priority
		// Use exponential decay with fixed rate, then multiply by weight
		// baseScore = 100 * (0.5 ^ index), finalScore = baseScore * weight
		// This ensures all card types have unique, non-zero scores
		const maxScoreValue = 100.0
		baseScore := maxScoreValue * math.Pow(nodeOrderDecayRate, float64(idx))
		score = baseScore * p.nodeOrderWeight

		klog.V(5).Infof(
			"NodeOrderFn: task <%s/%s> matched card type <%s> (priority index %d) on node <%s>, baseScore: %.2f, weight: %.2f, finalScore: %.2f",
			task.Namespace, task.Name, singleCardName, idx, node.Name, baseScore, p.nodeOrderWeight, score,
		)

		// Found the highest priority card type available on this node
		break
	}

	klog.V(4).Infof(
		"NodeOrderFn: task <%s/%s> with multi-card types <%s> on node <%s>, final score: %.2f",
		task.Namespace, task.Name, cardName, node.Name, score,
	)

	return score, nil
}
