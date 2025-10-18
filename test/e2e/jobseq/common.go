package jobseq

import (
	"context"
	"fmt"
	"os/exec"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func PruneUnusedImagesOnAllNodes(clientset *kubernetes.Clientset) error {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	for _, node := range nodes.Items {
		fmt.Printf("[Prune] Node: %s\n", node.Name)

		ctrCheckCmd := fmt.Sprintf("kubectl debug node/%s --image=busybox -- chroot /host sh -c 'test -S /run/containerd/containerd.sock'", node.Name)
		if err := exec.Command("bash", "-c", ctrCheckCmd).Run(); err == nil {
			cmd := fmt.Sprintf("kubectl debug node/%s --image=busybox -- chroot /host sh -c 'ctr -n k8s.io images prune -all || true'", node.Name)
			out, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			fmt.Printf("[CTR Prune Output]\n%s\n", string(out))
			continue
		}

		dockerCheckCmd := fmt.Sprintf("kubectl debug node/%s --image=busybox -- chroot /host sh -c 'docker version >/dev/null 2>&1'", node.Name)
		if err := exec.Command("bash", "-c", dockerCheckCmd).Run(); err == nil {
			cmd := fmt.Sprintf("kubectl debug node/%s --image=busybox -- chroot /host sh -c 'docker image prune -af || true'", node.Name)
			out, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			fmt.Printf("[Docker Prune Output]\n%s\n", string(out))
			continue
		}

		fmt.Printf("[Warning] Node %s: No known container runtime detected, skipping prune\n", node.Name)
	}

	return nil
}
