/*
Copyright 2019 The Volcano Authors.

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

package e2e

import (
	"encoding/json"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

var _ = Describe("Job E2E Test: Test Admission service", func() {

	It("Default queue would be added", func() {
		jobName := "job-default-queue"
		namespace := "test"
		context := initTestContext()
		defer cleanupTestContext(context)

		_, err := createJobInner(context, &jobSpec{
			min:       1,
			namespace: namespace,
			name:      jobName,
			tasks: []taskSpec{
				{
					img:  defaultNginxImage,
					req:  oneCPU,
					min:  1,
					rep:  1,
					name: "taskname",
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		createdJob, err := context.vkclient.BatchV1alpha1().Jobs(namespace).Get(jobName, v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(createdJob.Spec.Queue).Should(Equal("default"),
			"Job queue attribute would default to 'default' ")
	})

	It("Invalid CPU unit", func() {

		context := initTestContext()
		defer cleanupTestContext(context)
		namespace := "test"

		var job v1alpha1.Job
		jsonData := []byte(`{
   "apiVersion": "batch.volcano.sh/v1alpha1",
   "kind": "Job",
   "metadata": {
      "name": "test-job"
   },
   "spec": {
      "minAvailable": 3,
      "schedulerName": "volcano",
      "queue": "default",
      "tasks": [
         {
            "replicas": 3,
            "name": "default-nginx",
            "template": {
               "spec": {
                  "containers": [
                     {
                        "image": "nginx",
                        "imagePullPolicy": "IfNotPresent",
                        "name": "nginx",
                        "resources": {
                           "requests": {
                              "cpu": "-1"
                           }
                        }
                     }
                  ],
                  "restartPolicy": "Never"
               }
            }
         }
      ]
   }
}`)
		err := json.Unmarshal(jsonData, &job)
		Expect(err).NotTo(HaveOccurred())
		_, err = context.vkclient.BatchV1alpha1().Jobs(namespace).Create(&job)
		Expect(err).To(HaveOccurred())

	})

	It("Invalid memory unit", func() {

		context := initTestContext()
		defer cleanupTestContext(context)
		namespace := "test"

		var job v1alpha1.Job
		jsonData := []byte(`{
   "apiVersion": "batch.volcano.sh/v1alpha1",
   "kind": "Job",
   "metadata": {
      "name": "test-job"
   },
   "spec": {
      "minAvailable": 3,
      "schedulerName": "volcano",
      "queue": "default",
      "tasks": [
         {
            "replicas": 3,
            "name": "default-nginx",
            "template": {
               "spec": {
                  "containers": [
                     {
                        "image": "nginx",
                        "imagePullPolicy": "IfNotPresent",
                        "name": "nginx",
                        "resources": {
                           "requests": {
                              "memory": "-1"
                           }
                        }
                     }
                  ],
                  "restartPolicy": "Never"
               }
            }
         }
      ]
   }
}`)

		err := json.Unmarshal(jsonData, &job)
		Expect(err).NotTo(HaveOccurred())
		_, err = context.vkclient.BatchV1alpha1().Jobs(namespace).Create(&job)
		Expect(err).To(HaveOccurred())

	})

})
