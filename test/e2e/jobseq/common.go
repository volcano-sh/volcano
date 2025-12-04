package jobseq

import (
	"context"
	"fmt"
	"os/exec"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

func PruneUnusedImagesOnAllNodes(clientset *kubernetes.Clientset) error {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	for _, node := range nodes.Items {
		fmt.Printf("[Prune] Node: %s\n", node.Name)

		cmd := fmt.Sprintf("kubectl debug node/%s --attach --profile=general --image=%s -- chroot /host sh -c 'ctr -n k8s.io images prune -all || true'", node.Name, e2eutil.DefaultBusyBoxImage)
		out, err := exec.Command("bash", "-c", cmd).CombinedOutput()
		if err != nil {
			fmt.Printf("[Warning] Failed to run containerd image prune on node %s: %v. Output: %s\n", node.Name, err, string(out))
		}
		fmt.Printf("[CTR Prune Output]\n%s\n", string(out))
	}

	return nil
}
