
# Welcome to Volcano!

- [Before you get started](#before-you-get-started)
  - [Code of Conduct](#code-of-conduct)
  - [Community Expectations](#community-expectations)
- [Getting started](#getting-started)
- [Your First Contribution](#your-first-contribution)
  - [Find something to work on](#find-something-to-work-on)
    - [Find a good first topic](#find-a-good-first-topic)
    - [Work on an issue](#work-on-an-issue)
    - [File an Issue](#file-an-issue)
- [Contributor Workflow](#contributor-workflow)
  - [Creating Pull Requests](#creating-pull-requests)
  - [Code Review](#code-review)
  - [Testing](#testing)

# Before you get started

## Code of Conduct

Please make sure to read and observe our [Code of Conduct](./CODE_OF_CONDUCT.md).

## Community Expectations

Volcano is a community project driven by its community which strives to promote a healthy, friendly and productive environment.
The goal of the community is to develop a volcano system which is useful for running high performance workloads such as AI, ML, Deep Learning Application on top of Kubernetes. To build a volcano system at such scale requires the support of a community with similar aspirations.

- See [Community Membership](https://github.com/volcano-sh/community/blob/master/community-membership.md) for a list of various community roles. With gradual contributions, one can move up in the chain.


# Getting started

- Read the [get started](docs/development/prepare-for-development.md) for developing code for Volcano
- Read the [setup](docs/development/development.md) for build/deploy instructions.


# Your First Contribution

We will help you to contribute in different areas like filing issues, developing features, fixing critical bugs and getting your work reviewed and merged.

If you have questions about the development process, feel free to jump into our [Slack Channel](https://volcano-sh.slack.com/) or join our [mailing list](https://groups.google.com/forum/#!forum/volcano-sh).

## Find something to work on

We are always in need of help, be it fixing documentation, reporting bugs or writing some code.
Look at places where you feel best coding practices aren't followed, code refactoring is needed or tests are missing.
Here is how you get started.

### Find a good first topic

There are [multiple repositories](https://github.com/volcano-sh/) within the Volcano organization.
Each repository has beginner-friendly issues that provide a good first issue.
For example, [Volcano-Issues](https://github.com/volcano-sh/volcano/issues) has [help wanted](https://github.com/volcano-sh/volcano/issues?q=is%3Aopen+is%3Aissue+label%3A%22help+wanted%22) and [good first issue](https://github.com/volcano-sh/volcano/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22) labels for issues that should not need deep knowledge of the system.
We can help new contributors who wish to work on such issues.

Another good way to contribute is to find a documentation improvement, such as a missing/broken link.

#### Work on an issue

When you are willing to take on an issue, you can assign it to yourself. Just reply with `/assign` or `/assign @yourself` on an issue,
then the robot will assign the issue to you and your name will be present on the `Assignees` list.

### File an Issue

While we encourage everyone to contribute code, it is also appreciated when someone reports an issue.
Issues should be filed under the appropriate Volcano sub-repository.

*Example:* a Volcano issue should be opened to [Volcano](https://github.com/volcano-sh/volcano/issues).

Please follow the prompted submission guidelines while opening an issue.

Before submitting a bug report, reproduce the problem on a real Kubernetes
cluster with Volcano deployed. A local cluster such as kind or minikube counts:
it must run an actual Kubernetes control plane, nodes, and the affected Volcano
components. Trigger the problem through normal workloads or supported user
operations.

Keep the report focused on four things:

1. **Behavior:** the user scenario, actual result, and expected result.
2. **Environment:** Volcano release version or `master`, Kubernetes version,
   cluster setup, and relevant Volcano configuration. A commit ID alone is not
   sufficient; include it or an image digest as additional detail if available.
3. **Reproduction:** minimal workload manifests and commands another person can
   run on that setup. For intermittent failures, include the frequency and how
   long or how many attempts you ran.
4. **Evidence:** actual output from that cluster run, such as logs, events,
   resource status, or a stack trace. For performance problems, include cluster
   measurements or profiles with the workload scale and measurement procedure.

Unit tests, mocks, standalone benchmarks, static analysis, AI output, and code
path reasoning do not replace cluster reproduction. Root-cause analysis and a
suggested fix are optional; source references should use exact-commit permalinks.

Bug reports without a real-cluster reproduction and its evidence are not accepted
and will be closed. They can be reconsidered when that evidence is provided.

# Contributor Workflow

Please do not ever hesitate to ask a question or send a pull request.

This is a rough outline of what a contributor's workflow looks like:

- Create a topic branch from where to base the contribution. This is usually master.
- Make commits of logical units.
- Make sure commit messages are in the proper format (see below).
- Push changes in a topic branch to a personal fork of the repository.
- Submit a pull request to [Volcano](https://github.com/volcano-sh/volcano).
- The PR must receive an approval from two maintainers.

## Creating Pull Requests

Pull requests are often called simply "PR".
Volcano generally follows the standard [github pull request](https://help.github.com/articles/about-pull-requests/) process.

In addition to the above process, a bot will begin applying structured labels to your PR.

The bot may also make some helpful suggestions for commands to run in your PR to facilitate review.
These `/command` options can be entered in comments to trigger auto-labeling and notifications.
Refer to its [command reference documentation](https://go.k8s.io/bot-commands).

A bug-fix PR must link to an issue that meets the real-cluster reproduction
requirements in [File an Issue](#file-an-issue), or include the same evidence in
the PR. Reproduce the reported failure on the affected version before the fix.
A patch or regression test alone does not establish a bug. Bug-fix PRs without
the required cluster evidence will be closed along with unsupported bug reports.

Maintainers may ask for missing information or changes. They may close a PR that does not follow the PR template or these contribution requirements. They may also close it if the author does not provide the requested information or cannot explain the change.

## AI Guidance

Using AI tools to help prepare an issue or PR is acceptable. The author remains
responsible for every claim and change.

For an AI-assisted bug report or bug-fix PR, reproduce the failure on a real
cluster before submission and provide the evidence required by
[File an Issue](#file-an-issue). A failing unit test or an AI-generated explanation
is not sufficient. If you cannot reproduce the failure on a real cluster, do not
submit it as a bug or open a PR claiming to fix it.

Verify all claimed versions, code references, and results. Clearly distinguish
observed cluster behavior from a proposed explanation. Never present generated
or predicted logs, output, or reproduction steps as results you actually obtained.

For an AI-assisted PR, review every change and run the appropriate tests before
submission. Do not leave the first code review to the maintainers.

Reviewers may ask you to explain the analysis or code. The issue or PR may be
closed if you cannot support its claims or explain its changes. Opening a PR
does not prove that the linked issue is valid.

These responsibilities apply to reviewers as well as contributors. Read the
repository's [agent instructions](AGENTS.md) before using AI for implementation
or review. Understand the affected user workflow and inspect the code and
verification evidence before making claims about correctness. Reviewers must
distinguish author-provided results from checks they personally ran; passing
unit tests or another AI review is not enough to approve a runtime change.

AI may assist investigation, but authors and reviewers must write substantive
review comments and replies themselves. Do not delegate review discussions to
AI or run automated exchanges between reviewer and contributor agents. Address
the specific question with evidence, and state plainly when something has not
been verified.

Disclose the use of AI tools in the issue or in the PR's "Special notes for your
reviewer" section. All contributions must follow the contribution policies and
use commit messages that follow [the policy](#format-of-the-commit-message).
[Large AI-generated PRs](https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#large-or-automatic-edits)
and AI-generated commit messages are discouraged.

## Code Review

To make it easier for your PR to receive reviews, consider the reviewers will need you to:

* follow [good coding guidelines](https://go.dev/wiki/CodeReviewComments).
* write [good commit messages](https://chris.beams.io/posts/git-commit/).
* break large changes into a logical series of smaller patches which individually make easily understandable changes, and in aggregate solve a broader issue.
* label PRs with appropriate reviewers: to do this read the messages the bot sends you to guide you through the PR process.

### Format of the commit message

We follow a rough convention for commit messages that is designed to answer two questions: what changed and why.
The subject line should feature the what and the body of the commit should describe the why.

```
scripts: add test codes for metamanager

this adds some unit test codes to improve code coverage for metamanager

Fixes #12
```

The format can be described more formally as follows:

```
<subsystem>: <what changed>
<BLANK LINE>
<why this change was made>
<BLANK LINE>
<footer>
```

The first line is the subject and should be no longer than 70 characters, the second line is always blank, and other lines should be wrapped at 80 characters. This allows the message to be easier to read on GitHub as well as in various git tools.

Note: if your pull request isn't getting enough attention, you can use the reach out on Slack to get help finding reviewers.

## Testing

Follow the [testing guidelines](docs/development/development.md#about-testing)
when adding or modifying tests.

There are multiple types of tests.
The location of the test code varies with type, as do the specifics of the environment needed to successfully run the test:

* Unit: These confirm that a particular function behaves as intended. Unit test source code can be found adjacent to the corresponding source code within a given package. These are easily run locally by any developer.
* Integration: These tests cover interactions of package components or interactions between Volcano components and Kubernetes control plane components like API server.
* End-to-end ("e2e"): These are broad tests of overall system behavior and coherence. The e2e tests are in [Volcano e2e](https://github.com/volcano-sh/volcano/tree/master/test/e2e).

Continuous integration will run these tests on PRs.
