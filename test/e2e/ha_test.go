package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("High Availability Deployment Tests", func() {
	It("Should successfully spin up and report 2 ready replicas for core components", func() {
		components := []string{"volcano-scheduler", "volcano-controllers", "volcano-admission"}
		namespace := "volcano-system"

		for _, component := range components {
			Eventually(func() int32 {
				deployment, err := ctx.KubeClient().AppsV1().Deployments(namespace).Get(context.TODO(), component, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to get deployment for %s", component)
				
				return deployment.Status.ReadyReplicas
			}, 3*time.Minute, 5*time.Second).Should(Equal(int32(2)), "Component %s did not reach 2 ready replicas", component)
		}
	})
})