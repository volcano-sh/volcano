/*
Copyright 2017 The Kubernetes Authors.
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

package options

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voptions "volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/util"
)

func TestNewServerOption(t *testing.T) {
	tests := []struct {
		name string
		want *ServerOption
	}{
		{
			name: "creates new server option with defaults",
			want: &ServerOption{
				ServerOption: &voptions.ServerOption{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewServerOption()
			assert.NotNil(t, got)
			assert.NotNil(t, got.ServerOption)
		})
	}
}

func TestServerOption_AddFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantScheduler string
		wantShardMode string
		wantShardName string
		wantQPS       float32
		wantBurst     int
		wantMinNodes  int32
	}{
		{
			name:          "default values",
			args:          []string{},
			wantScheduler: defaultSchedulerName,
			wantShardMode: util.NoneShardingMode,
			wantShardName: defaultShardName,
			wantQPS:       defaultQPS,
			wantBurst:     defaultBurst,
			wantMinNodes:  defaultMinNodesToFind,
		},
		{
			name: "custom scheduler name",
			args: []string{
				"--scheduler-name=custom-scheduler",
			},
			wantScheduler: "custom-scheduler",
			wantShardMode: util.NoneShardingMode,
			wantShardName: defaultShardName,
		},
		{
			name: "custom sharding configuration",
			args: []string{
				"--scheduler-sharding-mode=hard",
				"--scheduler-sharding-name=shard-1",
			},
			wantScheduler: defaultSchedulerName,
			wantShardMode: "hard",
			wantShardName: "shard-1",
		},
		{
			name: "custom QPS and burst",
			args: []string{
				"--kube-api-qps=1000.0",
				"--kube-api-burst=1500",
			},
			wantScheduler: defaultSchedulerName,
			wantQPS:       1000.0,
			wantBurst:     1500,
		},
		{
			name: "custom minimum nodes",
			args: []string{
				"--minimum-feasible-nodes=200",
			},
			wantScheduler: defaultSchedulerName,
			wantMinNodes:  200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServerOption()
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			s.AddFlags(fs)

			err := fs.Parse(tt.args)
			require.NoError(t, err)

			if tt.wantScheduler != "" {
				assert.Equal(t, tt.wantScheduler, s.SchedulerName)
			}
			if tt.wantShardMode != "" {
				assert.Equal(t, tt.wantShardMode, s.ShardingMode)
			}
			if tt.wantShardName != "" {
				assert.Equal(t, tt.wantShardName, s.ShardName)
			}
			if tt.wantQPS != 0 {
				assert.Equal(t, tt.wantQPS, s.KubeClientOptions.QPS)
			}
			if tt.wantBurst != 0 {
				assert.Equal(t, tt.wantBurst, s.KubeClientOptions.Burst)
			}
			if tt.wantMinNodes != 0 {
				assert.Equal(t, tt.wantMinNodes, s.MinNodesToFind)
			}
		})
	}
}

func TestServerOption_RegisterOptions(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "registers server options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServerOption()
			s.RegisterOptions()

			assert.NotNil(t, ServerOpts)
			assert.Equal(t, s, ServerOpts)
			assert.NotNil(t, voptions.ServerOpts)
		})
	}
}

func TestServerOption_readCAFiles(t *testing.T) {
	// Create temporary test files
	tmpDir := t.TempDir()

	caCertContent := []byte("test-ca-cert")
	certContent := []byte("test-cert")
	keyContent := []byte("test-key")

	caCertFile := filepath.Join(tmpDir, "ca.crt")
	certFile := filepath.Join(tmpDir, "tls.crt")
	keyFile := filepath.Join(tmpDir, "tls.key")

	require.NoError(t, os.WriteFile(caCertFile, caCertContent, 0644))
	require.NoError(t, os.WriteFile(certFile, certContent, 0644))
	require.NoError(t, os.WriteFile(keyFile, keyContent, 0644))

	tests := []struct {
		name        string
		caCertFile  string
		certFile    string
		keyFile     string
		wantErr     bool
		errContains string
	}{
		{
			name:       "successfully read all CA files",
			caCertFile: caCertFile,
			certFile:   certFile,
			keyFile:    keyFile,
			wantErr:    false,
		},
		{
			name:        "missing CA cert file",
			caCertFile:  filepath.Join(tmpDir, "nonexistent-ca.crt"),
			certFile:    certFile,
			keyFile:     keyFile,
			wantErr:     true,
			errContains: "failed to read cacert file",
		},
		{
			name:        "missing cert file",
			caCertFile:  caCertFile,
			certFile:    filepath.Join(tmpDir, "nonexistent.crt"),
			keyFile:     keyFile,
			wantErr:     true,
			errContains: "failed to read cert file",
		},
		{
			name:        "missing key file",
			caCertFile:  caCertFile,
			certFile:    certFile,
			keyFile:     filepath.Join(tmpDir, "nonexistent.key"),
			wantErr:     true,
			errContains: "failed to read key file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerOption{
				ServerOption: &voptions.ServerOption{},
			}
			s.CaCertFile = tt.caCertFile
			s.CertFile = tt.certFile
			s.KeyFile = tt.keyFile

			err := s.readCAFiles()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, caCertContent, s.CaCertData)
				assert.Equal(t, certContent, s.CertData)
				assert.Equal(t, keyContent, s.KeyData)
			}
		})
	}
}

func TestServerOption_ParseCAFiles(t *testing.T) {
	tmpDir := t.TempDir()

	caCertFile := filepath.Join(tmpDir, "ca.crt")
	certFile := filepath.Join(tmpDir, "tls.crt")
	keyFile := filepath.Join(tmpDir, "tls.key")

	require.NoError(t, os.WriteFile(caCertFile, []byte("ca-cert"), 0644))
	require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0644))
	require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0644))

	tests := []struct {
		name        string
		setupFiles  bool
		decryptFunc DecryptFunc
		wantErr     bool
		errContains string
	}{
		{
			name:        "parse without decrypt function",
			setupFiles:  true,
			decryptFunc: nil,
			wantErr:     false,
		},
		{
			name:       "parse with successful decrypt function",
			setupFiles: true,
			decryptFunc: func(c *ServerOption) error {
				// Mock decryption - just append "-decrypted"
				c.CaCertData = append(c.CaCertData, []byte("-decrypted")...)
				return nil
			},
			wantErr: false,
		},
		{
			name:       "parse with failing decrypt function",
			setupFiles: true,
			decryptFunc: func(c *ServerOption) error {
				return assert.AnError
			},
			wantErr: true,
		},
		{
			name:        "parse with missing files",
			setupFiles:  false,
			decryptFunc: nil,
			wantErr:     true,
			errContains: "failed to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ServerOption{
				ServerOption: &voptions.ServerOption{},
			}

			if tt.setupFiles {
				s.CaCertFile = caCertFile
				s.CertFile = certFile
				s.KeyFile = keyFile
			} else {
				s.CaCertFile = filepath.Join(tmpDir, "missing.crt")
				s.CertFile = certFile
				s.KeyFile = keyFile
			}

			err := s.ParseCAFiles(tt.decryptFunc)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "creates default server option",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Default()
			assert.NotNil(t, got)
			assert.NotNil(t, got.ServerOption)
			assert.NotNil(t, ServerOpts)
		})
	}
}

func TestDefaultConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{
			name:     "defaultSchedulerName",
			got:      defaultSchedulerName,
			expected: "agent-scheduler",
		},
		{
			name:     "defaultShardName",
			got:      defaultShardName,
			expected: "agent-scheduler",
		},
		{
			name:     "defaultSchedulerPeriod",
			got:      defaultSchedulerPeriod,
			expected: time.Second,
		},
		{
			name:     "defaultResyncPeriod",
			got:      defaultResyncPeriod,
			expected: 0,
		},
		{
			name:     "defaultQPS",
			got:      defaultQPS,
			expected: 2000.0,
		},
		{
			name:     "defaultBurst",
			got:      defaultBurst,
			expected: 2000,
		},
		{
			name:     "defaultMinPercentageOfNodesToFind",
			got:      defaultMinPercentageOfNodesToFind,
			expected: 5,
		},
		{
			name:     "defaultMinNodesToFind",
			got:      defaultMinNodesToFind,
			expected: 100,
		},
		{
			name:     "defaultNodeWorkers",
			got:      defaultNodeWorkers,
			expected: 20,
		},
		{
			name:     "defaultScheduleWorkerCount",
			got:      defaultScheduleWorkerCount,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.got)
		})
	}
}
