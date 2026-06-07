# How to cherry-pick PRs

This document explains how cherry picks are managed on release branches within
the `volcano-sh/volcano` repository.
A common use case for this task is backporting PRs from master to release
branches.

> This doc is lifted from [Kubernetes cherry-pick](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-release/cherry-picks.md).

- [Prerequisites](#prerequisites)
  - [Tooling](#tooling)
  - [GitHub Setup](#github-setup)
  - [Authentication](#authentication)
  - [Environment Variables](#environment-variables)
- [What Kind of PRs are Good for Cherry Picks](#what-kind-of-prs-are-good-for-cherry-picks)
- [Initiate a Cherry Pick](#initiate-a-cherry-pick)
- [Cherry Pick Review](#cherry-pick-review)
- [Troubleshooting Cherry Picks](#troubleshooting-cherry-picks)

## Prerequisites

Before you initiate a cherry-pick, you need to ensure your environment is correctly configured.

### Tooling
- **Git**: A working `git` installation.
- **GitHub CLI**: The `gh` command-line tool. You can find in [installation instructions](https://github.com/cli/cli#installation).

### GitHub Setup
- **A merged pull request**: The PR you want to cherry-pick must already be merged into the `master` branch.
- **An existing release branch**: The target branch for the cherry-pick must exist (e.g., [release-1.11](https://github.com/volcano-sh/volcano/tree/release-1.11)).
- **A personal fork**: You need a fork of the `volcano-sh/volcano` repository under your own GitHub account or organization.
- **Configured Git remotes**: You should have remotes configured for both the upstream repository and your fork. You can check with `git remote -v`.

### Authentication

You must be authenticated with GitHub for the script to work. The recommended way is to use the GitHub CLI's built-in authentication.

1.  Run the following command in your terminal:
    ```shell
    gh auth login
    ```
2.  Follow the on-screen prompts. The easiest method is usually to log in with a web browser.
3.  If you choose to authenticate with a personal access token, ensure it has the `repo` and `read:org` scopes.

### Environment Variables
The script relies on a few environment variables to function correctly:
- `GITHUB_USER`: Your GitHub username. This is mandatory and used to correctly target your fork when creating the cherry-pick pull request.
  ```shell
  export GITHUB_USER="your-github-username"
  ```
- `UPSTREAM_REMOTE` (optional, default: `upstream`): The name of your git remote that points to the main `volcano-sh/volcano` repository.
- `FORK_REMOTE` (optional, default: `origin`): The name of your git remote that points to your personal fork.

You can set these variables in your shell profile or prefix them to the command when you run it.

## What Kind of PRs are Good for Cherry Picks

Patch releases must be easy and safe to consume, so security fixes
and critical bugfixes can be delivered with minimal risk of regression.

Only the following types of changes are expected to be backported:

- Security fixes
    - Dependency updates that just aim to silence some scanners
      and do not fix any vulnerable code are **not** eligible for cherry-picks.
- Critical bug fixes (loss of data, memory corruption, panic, crash, hang)
- Test-only changes to stabilize failing / flaky tests on release branches

If you are proposing a cherry pick outside these categories, please reconsider.
If upon reflection you wish to continue, bolster your case by supplementing your PR with e.g.,

- A GitHub issue detailing the problem

- Scope of the change

- Risks of adding a change

- Risks of associated regression

- Testing performed, test cases added

- Key stakeholder reviewers/approvers attesting to their confidence in the
  change being a required backport

## Initiate a Cherry Pick

- Run the [cherry pick script](https://github.com/volcano-sh/volcano/blob/master/hack/cherry_pick_pull.sh).

  The script requires the target branch and the PRs as arguments. For example, to apply PR #4272 to `release-1.11`:

  ```shell
  hack/cherry_pick_pull.sh upstream/release-1.11 4272
  ```

  If your local git remote for the main repository is `origin` and your fork's remote is `downstream`, you could run:
  ```shell
  UPSTREAM_REMOTE=origin FORK_REMOTE=downstream hack/cherry_pick_pull.sh origin/release-1.11 4272
  ```
- You will need to run the cherry pick script separately for each patch
  release you want to cherry pick to. Cherry picks should be applied to all
  active release branches where the fix is applicable.

- If `GITHUB_TOKEN` is not set you will be asked for your github password:
  provide the github [personal access token](https://github.com/settings/tokens) rather than your actual github
  password. If you can securely set the environment variable `GITHUB_TOKEN`
  to your personal access token then you can avoid an interactive prompt.
  Refer [https://github.com/github/hub/issues/2655#issuecomment-735836048](https://github.com/github/hub/issues/2655#issuecomment-735836048)

## Cherry Pick Review

As with any other PR, code OWNERS review (`/lgtm`) and approve (`/approve`) on
cherry pick PRs as they deem appropriate.

The same release note requirements apply as normal pull requests, except the
release note stanza will auto-populate from the master branch pull request from
which the cherry pick originated.

## Troubleshooting Cherry Picks

Contributors may encounter some of the following difficulties when initiating a
cherry pick.

- A cherry pick PR does not apply cleanly against an old release branch. In
  that case, you will need to manually fix conflicts.

- The cherry pick PR includes code that does not pass CI tests. In such a case
  you will have to fetch the auto-generated branch from your fork, amend the
  problematic commit and force push to the auto-generated branch.
  Alternatively, you can create a new PR, which is noisier.