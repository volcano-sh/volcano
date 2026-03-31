package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var _ = Describe("High Availability Deployment Tests", func() {
	var ctx *e2eutil.TestContext

	BeforeEach(func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
	})

	AfterEach(func() {
		e2eutil.CleanupTestContext(ctx)
	})

	It("Should successfully spin up and report 2 ready replicas for core components", func() {
		components := []string{"volcano-scheduler", "volcano-controllers", "volcano-admission"}
		namespace := "volcano-system"

		for _, component := range components {
			Eventually(func() (int32, error) {
				deployment, err := ctx.Kubeclient.AppsV1().Deployments(namespace).Get(context.TODO(), component, metav1.GetOptions{})
				if err != nil {
					return 0, err
				}
				
				return deployment.Status.ReadyReplicas, nil
			}, 3*time.Minute, 5*time.Second).Should(Equal(int32(2)), "Component %s did not reach 2 ready replicas", component)
		}
	})
})