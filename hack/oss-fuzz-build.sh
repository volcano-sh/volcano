#!/bin/bash -eu

printf "package job\nimport _ \"github.com/AdamKorcz/go-118-fuzz-build/testing\"\n" >"$SRC"/volcano/pkg/controllers/job/register.go
go mod tidy
compile_native_go_fuzzer volcano.sh/volcano/pkg/controllers/job FuzzApplyPolicies FuzzApplyPolicies 
compile_native_go_fuzzer volcano.sh/volcano/pkg/controllers/job FuzzCreateJobPod FuzzCreateJobPod 
