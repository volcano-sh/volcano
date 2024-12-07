/*
Copyright 2024 The Volcano Authors.

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

package exec

import (
	"context"
	"fmt"
	"os/exec"
)

//go:generate mockgen -destination  ./mocks/mock_exec.go -package mocks -source exec.go

// ExecInterface is an interface of os/exec API.
type ExecInterface interface {
	// CommandContext runs command
	CommandContext(ctx context.Context, cmd string) (string, error)
}

var _ ExecInterface = &Executor{}

type Executor struct{}

var executor ExecInterface

func GetExecutor() ExecInterface {
	if executor == nil {
		executor = &Executor{}
	}
	return executor
}

func SetExecutor(e ExecInterface) {
	executor = e
}

func (e *Executor) CommandContext(ctx context.Context, cmd string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("command failed, nil Context")
	}
	var outBytes []byte
	var err error
	outBytes, err = exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
	outputStr := ""
	if len(outBytes) != 0 {
		outputStr = string(outBytes)
	}
	return outputStr, err
}
