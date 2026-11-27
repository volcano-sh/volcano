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

package file

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestReadIntFromFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int64
		wantErr bool
	}{
		{name: "value with trailing newline", content: "123456\n", want: 123456},
		{name: "value without newline", content: "42", want: 42},
		{name: "negative value", content: "-1\n", want: -1},
		{name: "value larger than int32", content: "9876543210\n", want: 9876543210},
		{name: "not a number", content: "max\n", wantErr: true},
		{name: "empty file", content: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestFile(t, "value", tt.content)

			got, err := ReadIntFromFile(path)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("missing file", func(t *testing.T) {
		_, err := ReadIntFromFile(filepath.Join(t.TempDir(), "missing"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestReadBatchFromFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	missing := filepath.Join(dir, "missing")
	require.NoError(t, os.WriteFile(first, []byte("1\n"), 0644))
	require.NoError(t, os.WriteFile(second, []byte(""), 0644))

	got := ReadBatchFromFile([]string{first, missing, second})

	assert.Equal(t, map[string]string{first: "1\n", second: ""}, got)
}

func TestWriteByteToFile(t *testing.T) {
	path := writeTestFile(t, "value", "old content that is longer")
	content := []byte("new")

	require.NoError(t, WriteByteToFile(path, content))

	got, err := ReadByteFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}
