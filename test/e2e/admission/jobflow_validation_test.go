/*
Copyright 2025 The Volcano Authors.

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

package admission

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("JobFlow Validating E2E Test", func() {

	ginkgo.It("Should allow jobflow creation with valid DAG structure", func() {
		jobFlowName := "valid-dag-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "a",
					},
					{
						Name: "b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"},
						},
					},
					{
						Name: "c",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a", "b"},
						},
					},
					{
						Name: "d",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"b"},
						},
					},
					{
						Name: "e",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"c", "d"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow creation with duplicate flow names", func() {
		jobFlowName := "duplicate-names-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "a",
					},
					{
						Name: "a", // Duplicate name
					},
					{
						Name: "b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject jobflow creation with missing flow definition", func() {
		jobFlowName := "missing-flow-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"}, // "a" is not defined
						},
					},
					{
						Name: "c",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a", "b"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("jobflow Flow is not DAG"))
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("vertex is not defined"))
	})

	ginkgo.It("Should allow jobflow creation with multiple flows having same dependency target", func() {
		jobFlowName := "multi-dependency-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "a",
					},
					{
						Name: "b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"},
						},
					},
					{
						Name: "c",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"}, // Same dependency as "b"
						},
					},
					{
						Name: "c", // Duplicate name with different dependency
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"b"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow update with valid DAG structure", func() {
		jobFlowName := "update-valid-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create initial jobflow
		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "a",
					},
					{
						Name: "b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"a"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		createdJobFlow, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Update with valid DAG structure
		createdJobFlow.Spec.Flows = append(createdJobFlow.Spec.Flows, flowv1alpha1.Flow{
			Name: "c",
			DependsOn: &flowv1alpha1.DependsOn{
				Targets: []string{"b"},
			},
		})

		_, err = testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Update(context.TODO(), createdJobFlow, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow creation with simple linear dependency chain", func() {
		jobFlowName := "linear-dependency-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "start",
					},
					{
						Name: "middle",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"start"},
						},
					},
					{
						Name: "end",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"middle"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow creation with complex valid DAG structure", func() {
		jobFlowName := "complex-dag-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "root1",
					},
					{
						Name: "root2",
					},
					{
						Name: "level1-a",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"root1"},
						},
					},
					{
						Name: "level1-b",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"root2"},
						},
					},
					{
						Name: "level2",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"level1-a", "level1-b"},
						},
					},
					{
						Name: "final",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"level2"},
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow creation with empty flows list", func() {
		jobFlowName := "empty-flows-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows:           []flowv1alpha1.Flow{}, // Empty flows
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow jobflow creation with single flow (no dependencies)", func() {
		jobFlowName := "single-flow-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "single-flow",
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject jobflow creation with self-dependency", func() {
		jobFlowName := "self-dependency-jobflow"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		jobFlow := &flowv1alpha1.JobFlow{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testCtx.Namespace,
				Name:      jobFlowName,
			},
			Spec: flowv1alpha1.JobFlowSpec{
				Flows: []flowv1alpha1.Flow{
					{
						Name: "self-dependent",
						DependsOn: &flowv1alpha1.DependsOn{
							Targets: []string{"self-dependent"}, // Self dependency
						},
					},
				},
				JobRetainPolicy: flowv1alpha1.RetainPolicy("retain"),
			},
		}

		_, err := testCtx.Vcclient.FlowV1alpha1().JobFlows(testCtx.Namespace).Create(context.TODO(), jobFlow, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("jobflow Flow is not DAG"))
	})
})
