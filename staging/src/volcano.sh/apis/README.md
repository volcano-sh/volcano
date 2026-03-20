> ⚠️ **This is a staged repository that is automatically synced to [volcano-sh/apis](https://github.com/volcano-sh/apis).**
>
> Contributions, including issues and pull requests, should be made to the main Volcano repository: [https://github.com/volcano-sh/volcano](https://github.com/volcano-sh/volcano).
>
> The [volcano-sh/apis](https://github.com/volcano-sh/apis) repository is read-only and used for importing only.

## Introduction

volcano-sh/apis provides CRD types, informers, listers, and clientsets for Volcano custom resources.

## For Users

To use Volcano APIs in your project:

```go
import (
    vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
    vcscheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
    vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
)
```

## For Developers

### Making API Changes

1. Clone the main Volcano repository:
   ```shell
   git clone https://github.com/volcano-sh/volcano.git
   ```

2. Make changes in `staging/src/volcano.sh/apis/`

3. Regenerate code if needed:
   ```shell
   cd staging/src/volcano.sh/apis
   bash ./hack/update-codegen.sh
   ```

4. Submit a PR to the main Volcano repository

5. After merge, changes will be automatically synced to [volcano-sh/apis](https://github.com/volcano-sh/apis)

### Repository Structure

**This staging directory contains only the API code.** Repository-specific files are maintained separately:

- **Maintained in staging (synced to apis repo):**
  - `pkg/` - API definitions and generated code
  - `hack/` - Code generation scripts
  - `go.mod`, `go.sum` - Go module files
  - `README.md` - Basic documentation
  - Community files: `code_of_conduct.md`, `community-membership.md`, `contribute.md`

- **Maintained only in [volcano-sh/apis](https://github.com/volcano-sh/apis) repo:**
  - `.github/` - GitHub workflows, issue templates
  - `.gitignore` - Repository-specific ignore patterns
  - `GOVERNANCE.md` - Governance documentation
  - `OWNERS` - Maintainer list
  - `SECURITY.md` - Security policy
  - `LICENSE` - License file

This separation ensures that repository governance and configuration remain independent between the main Volcano repository and the standalone APIs repository.
