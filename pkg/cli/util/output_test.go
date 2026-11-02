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

package util_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	flow "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/volcano/pkg/cli/jobtemplate"
	"volcano.sh/volcano/pkg/cli/pod"
	"volcano.sh/volcano/pkg/cli/vjobs"
)

// errorWriter simulates an output destination failing on a header or data row.
type errorWriter struct {
	bytes.Buffer
	failOn int
	writes int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failOn {
		return 0, io.ErrClosedPipe
	}
	return w.Buffer.Write(p)
}

// These tests change process-wide streams and must not run in parallel.
func captureStreams(t *testing.T, run func()) (string, string) {
	t.Helper()
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	defer stdout.Close()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	require.NoError(t, err)
	defer stderr.Close()
	savedOut, savedErr := os.Stdout, os.Stderr
	func() {
		os.Stdout, os.Stderr = stdout, stderr
		defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()
		run()
	}()
	_, err = stdout.Seek(0, io.SeekStart)
	require.NoError(t, err)
	out, err := io.ReadAll(stdout)
	require.NoError(t, err)
	_, err = stderr.Seek(0, io.SeekStart)
	require.NoError(t, err)
	diagnostic, err := io.ReadAll(stderr)
	require.NoError(t, err)
	return string(out), string(diagnostic)
}

func TestPrinterErrorStreams(t *testing.T) {
	tests := []struct {
		name       string
		print      func(io.Writer)
		diagnostic string
	}{
		{"jobs", func(w io.Writer) {
			vjobs.PrintJobs(&batch.JobList{Items: []batch.Job{{ObjectMeta: metav1.ObjectMeta{Name: "example"}}}}, w)
		}, "Failed to print list command result:"},
		{"pods", func(w io.Writer) {
			pod.PrintPods(&corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "example"}}}}, w, false)
		}, "Failed to print Pod information:"},
		{"pods all namespaces", func(w io.Writer) {
			pod.PrintPods(&corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default"}}}}, w, true)
		}, "Failed to print Pod information:"},
		{"job templates", func(w io.Writer) {
			jobtemplate.PrintJobTemplates(&flow.JobTemplateList{Items: []flow.JobTemplate{{ObjectMeta: metav1.ObjectMeta{Name: "example"}}}}, w)
		}, "Failed to print JobTemplate command result:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, output := range []struct {
				name   string
				failOn int
			}{
				{"success", 0}, {"header failure", 1}, {"row failure", 2},
			} {
				t.Run(output.name, func(t *testing.T) {
					writer := &errorWriter{failOn: output.failOn}
					stdout, stderr := captureStreams(t, func() { tt.print(writer) })
					assert.Empty(t, stdout, "diagnostics must not leak into stdout")
					if output.failOn == 0 {
						assert.Empty(t, stderr)
						assert.Contains(t, writer.String(), "example")
					} else {
						assert.Contains(t, stderr, tt.diagnostic)
						assert.Contains(t, stderr, io.ErrClosedPipe.Error())
					}
				})
			}
		})
	}
}

func TestGetEventsClientErrorStream(t *testing.T) {
	// A missing CA file is a configuration error detected before network access.
	caFile := filepath.Join(t.TempDir(), "missing-ca.crt")
	config := &rest.Config{Host: "https://localhost", TLSClientConfig: rest.TLSClientConfig{CAFile: caFile}}
	var events []corev1.Event
	stdout, stderr := captureStreams(t, func() {
		events = vjobs.GetEvents(context.Background(), config, &batch.Job{})
	})
	assert.Nil(t, events)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, caFile)
}
