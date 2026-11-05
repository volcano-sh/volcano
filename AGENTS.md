# Repository guidelines

## Before implementation or review

Read [CONTRIBUTING.md](CONTRIBUTING.md), including its bug-reporting and AI
guidance, before preparing an issue, implementing a change, or reviewing a PR.
These requirements apply to both contributors and reviewers.

Trace the affected behavior from a user operation or cluster event through the
actual callers and component lifecycle. Check relevant configuration, ownership,
event ordering, and retries. Explain how users reach the behavior; an isolated
function or a synthetic test does not establish the production path.

## Verification

- Before submitting a bug report or bug-fix PR, reproduce the failure on a real
  Kubernetes cluster with Volcano deployed. Local kind or minikube clusters count.
  Use normal workloads or supported user operations, not mocks or direct calls to
  internal functions. Without cluster reproduction, do not present a hypothesis
  as a confirmed bug or submit a PR claiming to fix it.
- For a bug fix, run the same reproduction against the affected version and the
  patched build. For other runtime behavior changes, exercise the affected user
  workflow on a real cluster before claiming the change is ready for review.
  Documentation-only changes do not require a cluster run.
- Report the versions or commits, configuration, manifests, commands, and actual
  results. Distinguish unit tests, cluster tests, code inspection, and checks not
  run. If verification is blocked, state what is missing; do not call the change
  verified. Follow the [development guide](docs/development/development.md) for
  applicable tests and verification commands.
- Passing unit tests, standalone benchmarks, static analysis, and agreement from
  another AI are not substitutes for cluster evidence. Never invent output or
  present predicted results as an executed test.

## Code Review Rules

- Check the user workflow and evidence before endorsing the fix. Inspect the
  relevant callers and lifecycle, not just the changed lines or new tests.
- Flag missing cluster reproduction or fix verification explicitly. Do not
  recommend approval of a runtime change based only on passing unit tests or
  another reviewer's conclusion.
- Ground correctness findings in a reachable user scenario and concrete evidence.
  Mark unverified concerns as hypotheses. Reviewers may request missing evidence
  without reproducing it themselves, but must distinguish author-provided results
  from checks they personally ran.

## Review discussions

AI may assist investigation, but the human author and reviewer must understand
the code, inspect the evidence, and write substantive review comments and replies
themselves. Do not generate or post review-thread replies on their behalf, run
automated back-and-forth between reviewer and contributor agents, or generate
approval and resolution claims as a substitute for human review.

Address the specific question with the relevant code, reasoning, change, or test
result. If the answer is unknown or a check was not run, say so. Do not repeat
generated explanations or claim a concern is resolved without checking it.

## Unit tests

Before adding or modifying unit tests, read and follow the
[unit-test standards](docs/development/development.md#unit-test-standards).
