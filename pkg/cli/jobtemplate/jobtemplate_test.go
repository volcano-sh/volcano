package jobtemplate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestListJobTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		Response    interface{}
		Namespace   string
		ExpectedErr error
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplateList{
				Items: []flowv1alpha1.JobTemplate{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-jobtemplate",
							Namespace: "default",
						},
					},
				},
			},
			Namespace:   "default",
			ExpectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			listJobTemplateFlags.Master = server.URL
			listJobTemplateFlags.Namespace = testCase.Namespace

			err := ListJobTemplate(context.TODO())
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
		})
	}
}

func TestGetJobTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		Response    *flowv1alpha1.JobTemplate
		Namespace   string
		Name        string
		ExpectedErr error
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobtemplate",
					Namespace: "default",
				},
			},
			Namespace:   "default",
			Name:        "test-jobtemplate",
			ExpectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			getJobTemplateFlags.Master = server.URL
			// Set the namespace and name as the flags
			getJobTemplateFlags.Namespace = testCase.Namespace
			getJobTemplateFlags.Name = testCase.Name
			err := GetJobTemplate(context.TODO())
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
		})
	}
}

func TestDeleteJobTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		Response    *flowv1alpha1.JobTemplate
		Namespace   string
		Name        string
		FilePath    string
		ExpectedErr error
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobtemplate",
					Namespace: "default",
				},
			},
			Namespace:   "default",
			Name:        "test-jobtemplate",
			ExpectedErr: nil,
		},
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobtemplate",
					Namespace: "default",
				},
			},
			FilePath:    "test.yaml",
			ExpectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			deleteJobTemplateFlags.Master = server.URL
			deleteJobTemplateFlags.Namespace = testCase.Namespace
			deleteJobTemplateFlags.Name = testCase.name
			deleteJobTemplateFlags.FilePath = testCase.FilePath

			if testCase.FilePath != "" {
				err := createAndWriteFile(testCase.FilePath, content)
				if err != nil {
					t.Fatalf("Failed to create and write file: %v", err)
				}
				// Delete the file after the test
				defer func() {
					err := os.Remove(testCase.FilePath)
					if err != nil {
						t.Fatalf("Failed to remove file: %v", err)
					}
				}()
			}

			err := DeleteJobTemplate(context.TODO())
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
		})
	}
}

func TestCreateJobTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		Response    *flowv1alpha1.JobTemplate
		FilePath    string
		ExpectedErr error
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobtemplate",
					Namespace: "default",
				},
			},
			FilePath:    "test.yaml",
			ExpectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			createJobTemplateFlags.Master = server.URL
			createJobTemplateFlags.FilePath = testCase.FilePath

			if testCase.FilePath != "" {
				err := createAndWriteFile(testCase.FilePath, content)
				if err != nil {
					t.Fatalf("Failed to create and write file: %v", err)
				}
				// Delete the file after the test
				defer func() {
					err := os.Remove(testCase.FilePath)
					if err != nil {
						t.Fatalf("Failed to remove file: %v", err)
					}
				}()
			}

			err := CreateJobTemplate(context.TODO())
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
			}
		})
	}
}

func TestDescribeJobTemplate(t *testing.T) {
	testCases := []struct {
		name        string
		Response    *flowv1alpha1.JobTemplate
		Namespace   string
		Name        string
		Format      string
		ExpectedErr error
	}{
		{
			name: "Normal Case",
			Response: &flowv1alpha1.JobTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jobtemplate",
					Namespace: "default",
				},
			},
			Namespace:   "default",
			Name:        "test-jobtemplate",
			Format:      "yaml",
			ExpectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := createTestServer(testCase.Response)
			defer server.Close()
			// Set the server URL as the master flag
			describeJobTemplateFlags.Master = server.URL
			describeJobTemplateFlags.Namespace = testCase.Namespace
			describeJobTemplateFlags.Name = testCase.name
			describeJobTemplateFlags.Format = testCase.Format

			err := DescribeJobTemplate(context.TODO())
			if !reflect.DeepEqual(err, testCase.ExpectedErr) {
				t.Fatalf("test case: %s failed: got: %v, want: %v", testCase.name, err, testCase.ExpectedErr)
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

func createAndWriteFile(filePath, content string) error {
	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 文件不存在，创建文件
		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		// 写入内容
		_, err = io.WriteString(file, content)
		if err != nil {
			return err
		}
	}

	return nil
}

func TestInitCreateFlags(t *testing.T) {
	var cmd cobra.Command
	InitCreateFlags(&cmd)

	if cmd.Flag("file") == nil {
		t.Errorf("Could not find the flag file")
	}
}

func TestInitGetFlags(t *testing.T) {
	var cmd cobra.Command
	InitGetFlags(&cmd)

	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag name")
	}
}

func TestInitListFlags(t *testing.T) {
	var cmd cobra.Command
	InitListFlags(&cmd)

	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
}

func TestInitDescribeFlags(t *testing.T) {
	var cmd cobra.Command
	InitDescribeFlags(&cmd)
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("format") == nil {
		t.Errorf("Could not find the flag format")
	}
}

func TestInitDeleteFlags(t *testing.T) {
	var cmd cobra.Command
	InitDeleteFlags(&cmd)
	if cmd.Flag("name") == nil {
		t.Errorf("Could not find the flag name")
	}
	if cmd.Flag("namespace") == nil {
		t.Errorf("Could not find the flag namespace")
	}
	if cmd.Flag("file") == nil {
		t.Errorf("Could not find the flag file")
	}
}

var content = `apiVersion: flow.volcano.sh/v1alpha1
kind: JobTemplate
metadata:
  name: a
  namespace: default
spec:
  minAvailable: 1
  schedulerName: volcano
  priorityClassName: high-priority
  policies:
    - event: PodEvicted
      action: RestartJob
  plugins:
    ssh: []
    env: []
    svc: []
  maxRetry: 5
  queue: default
  tasks:
    - replicas: 1
      name: "default-nginx"
      template:
        metadata:
          name: web
        spec:
          containers:
            - image: nginx:1.14.2
              command:
                - sh
                - -c
                - sleep 10s
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
---
apiVersion: flow.volcano.sh/v1alpha1
kind: JobTemplate
metadata:
  name: b
spec:
  minAvailable: 1
  schedulerName: volcano
  priorityClassName: high-priority
  policies:
    - event: PodEvicted
      action: RestartJob
  plugins:
    ssh: []
    env: []
    svc: []
  maxRetry: 5
  queue: default
  tasks:
    - replicas: 1
      name: "default-nginx"
      template:
        metadata:
          name: web
        spec:
          containers:
            - image: nginx:1.14.2
              command:
                - sh
                - -c
                - sleep 10s
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
---`
