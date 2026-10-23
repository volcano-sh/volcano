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

package filewatcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileWatcher(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) string
		wantErr bool
	}{
		{
			name: "existing file",
			prepare: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte("key: value\n"), 0644))
				return path
			},
		},
		{
			name: "existing directory",
			prepare: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "subdir")
				require.NoError(t, os.Mkdir(path, 0755))
				return path
			},
		},
		{
			name: "missing path",
			prepare: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "missing.yaml")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.prepare(t, t.TempDir())

			watcher, err := NewFileWatcher(path)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, watcher)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, watcher)
			defer watcher.Close()

			assert.NotNil(t, watcher.Events())
			assert.NotNil(t, watcher.Errors())
		})
	}
}

func TestFileWatcherEvents(t *testing.T) {
	tests := []struct {
		name   string
		action func(t *testing.T, path string)
		wantOp fsnotify.Op
	}{
		{
			name: "write to watched file",
			action: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0644))
			},
			wantOp: fsnotify.Write,
		},
		{
			name: "remove watched file",
			action: func(t *testing.T, path string) {
				require.NoError(t, os.Remove(path))
			},
			wantOp: fsnotify.Remove,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "watched.yaml")
			require.NoError(t, os.WriteFile(path, []byte("initial\n"), 0644))

			watcher, err := NewFileWatcher(path)
			require.NoError(t, err)
			defer watcher.Close()

			tt.action(t, path)

			timeout := time.After(5 * time.Second)
		wait:
			for {
				select {
				case event, ok := <-watcher.Events():
					if !ok {
						t.Fatal("events channel closed unexpectedly")
					}
					if event.Op&tt.wantOp != 0 {
						break wait
					}
				case err, ok := <-watcher.Errors():
					if !ok {
						t.Fatal("errors channel closed unexpectedly")
					}
					t.Fatalf("unexpected watcher error: %v", err)
				case <-timeout:
					t.Fatalf("timed out waiting for %v event", tt.wantOp)
				}
			}
		})
	}
}

func TestFileWatcherNilAndEmpty(t *testing.T) {
	tests := []struct {
		name    string
		watcher *fileWatcher
	}{
		{
			name:    "nil watcher",
			watcher: nil,
		},
		{
			name:    "watcher with nil fsnotify watcher",
			watcher: &fileWatcher{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Nil(t, tt.watcher.Events())
			assert.Nil(t, tt.watcher.Errors())
			assert.NotPanics(t, tt.watcher.Close)
		})
	}
}
