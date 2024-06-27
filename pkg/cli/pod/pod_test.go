package pod

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestListPod(t *testing.T) {
	testCases := []struct {
		name           string
		Response       interface{}
		Namespace      string
		JobName        string
		QueueName      string
		ExpectedErr    error
		ExpectedOutput string
	}{
		{
			name: "Normal Case",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-pod",
							Namespace: "default",
							Labels: map[string]string{
								v1alpha1.JobNameKey: "my-job",
							},
							CreationTimestamp: metav1.Now(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "my-container",
									Image: "nginx",
								},
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			Namespace:   "default",
			JobName:     "",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        0s`,
		},
		{
			name: "Normal Case with namespace filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-pod",
							Namespace: "default",
							Labels: map[string]string{
								v1alpha1.JobNameKey: "my-job",
							},
							CreationTimestamp: metav1.Now(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "my-container",
									Image: "nginx",
								},
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			Namespace:   "default",
			JobName:     "",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        0s`,
		},
		{
			name: "Normal Case with jobName filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-pod",
							Namespace: "default",
							Labels: map[string]string{
								v1alpha1.JobNameKey: "my-job1",
							},
							CreationTimestamp: metav1.Now(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "my-container",
									Image: "nginx",
								},
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			Namespace:   "default",
			JobName:     "my-job1",
			ExpectedErr: nil,
			ExpectedOutput: `Name          Ready      Status         Restart  Age       
my-pod        0/1        Running        0        0s`,
		},
		{
			name: "Normal Case with queueName filter",
			Response: &corev1.PodList{
				Items: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-pod1",
							Namespace: "default",
							Labels: map[string]string{
								v1alpha1.QueueNameKey: "my-queue1",
							},
							CreationTimestamp: metav1.Now(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "my-container",
									Image: "nginx",
								},
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "my-pod3",
							Namespace: "default",
							Labels: map[string]string{
								v1alpha1.JobNameKey:   "my-job2",
								v1alpha1.QueueNameKey: "my-queue1",
							},
							CreationTimestamp: metav1.Now(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "my-container",
									Image: "nginx",
								},
							},
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			Namespace:   "default",
			QueueName:   "my-queue1",
			ExpectedErr: nil,
			ExpectedOutput: `Name           Ready      Status         Restart  Age       
my-pod1        0/1        Running        0        0s        
my-pod3        0/1        Running        0        0s`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			listPodFlags.Master = server.URL
			listPodFlags.Namespace = testCase.Namespace
			listPodFlags.JobName = testCase.JobName
			listPodFlags.QueueName = testCase.QueueName
			listPodFlags.Namespace = testCase.Namespace
			r, oldStdout := redirectStdout()
			defer r.Close()

			err := ListPods(context.TODO())
			gotOutput := captureOutput(r, oldStdout)

			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
			if gotOutput != testCase.ExpectedOutput {
				t.Errorf("test case: %s failed: got: %s, want: %s", testCase.name, gotOutput, testCase.ExpectedOutput)
			}
		})
	}
}

func createTestServer(response interface{}) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	return server
}

// redirectStdout redirects os.Stdout to a pipe and returns the read and write ends of the pipe.
func redirectStdout() (*os.File, *os.File) {
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	return r, oldStdout
}

// captureOutput reads from r until EOF and returns the result as a string.
func captureOutput(r *os.File, oldStdout *os.File) string {
	w := os.Stdout
	os.Stdout = oldStdout
	w.Close()
	gotOutput, _ := io.ReadAll(r)
	return strings.TrimSpace(string(gotOutput))
}
