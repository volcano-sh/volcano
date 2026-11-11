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

package api

import "sync/atomic"

// DiagnosticVerbosity controls how much detail scheduling failure messages carry.
type DiagnosticVerbosity string

const (
	// DiagnosticVerbosityDetailed keeps node counts, per-reason counts and candidate node names.
	DiagnosticVerbosityDetailed DiagnosticVerbosity = "detailed"
	// DiagnosticVerbositySummary drops counts and candidate node names so that
	// repeated failures for the same reasons produce identical messages.
	DiagnosticVerbositySummary DiagnosticVerbosity = "summary"
)

var summaryDiagnostics atomic.Bool

// SetDiagnosticVerbosity selects the message verbosity. It is meant to be called once at scheduler startup.
func SetDiagnosticVerbosity(v DiagnosticVerbosity) {
	summaryDiagnostics.Store(v == DiagnosticVerbositySummary)
}

// IsSummaryDiagnostics reports whether failure messages should omit counts and candidate node names.
func IsSummaryDiagnostics() bool {
	return summaryDiagnostics.Load()
}
