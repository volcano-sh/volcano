# Things to do when volcano adapts to k8s version upgrade

## Motivation

In order to ensure that volcano is compatible with the new version of kubernetes, the k8s version of volcano needs to be routinely upgraded.

## Upgrade Process

> **Note**: All API development and upgrades are done in the main volcano repository. The [volcano-sh/apis](https://github.com/volcano-sh/apis) repository is automatically synced via the `sync-apis` workflow.

### Upgrade volcano apis (in staging directory)

> **Note**: This is the first step in the upgrade process. All changes (staging APIs + main repository) will be included in a **single PR**.

Since `volcano.sh/apis` is maintained in `staging/src/volcano.sh/apis/`, all API upgrades are done directly in the volcano repository. The main things that need to be done are as follows:

1. **Upgrade staging APIs go.mod** (`staging/src/volcano.sh/apis/go.mod`):
    - Update k8s.io dependencies to the target k8s version
    - Update k8s.io/code-generator version if needed
    - Run `go mod tidy` in the staging directory

2. **Upgrade Go version** in staging APIs:
    - Update Go version in `staging/src/volcano.sh/apis/go.mod` to match k8s requirements
    - Update Go version in code generation scripts if needed

3. **Regenerate code** after dependency updates:
   ```bash
   cd staging/src/volcano.sh/apis
   bash hack/update-codegen.sh
   bash hack/verify-codegen.sh  # Verify generated code is up to date
   ```

4. **Test staging APIs**:
   ```bash
   cd staging/src/volcano.sh/apis
   go build ./...
   go test ./...
   ```

### Upgrade volcano main repository

> **Important**: Both staging APIs and main repository upgrades should be done in the **same PR**. After upgrading the staging APIs in the same branch, continue with upgrading the main volcano repository. All changes will be included in a single PR for review.

The main things that need to be done are as follows:

1. **Upgrade main go.mod** to adapt to the k8s version:
    - Update all k8s.io dependencies to the target version
    - The `replace volcano.sh/apis => ./staging/src/volcano.sh/apis` directive ensures the staging APIs are used
    - Run `go mod tidy`

2. **Upgrade Go version and build tools**:
    - Update Go version in `go.mod` to match k8s requirements
    - Update `KUBE_VERSION` in Dockerfiles to keep consistent with k8s
    - Update Go version in GitHub Actions workflows if needed

3. **Regenerate main project code**:
   ```bash
   bash hack/update-gencode.sh
   bash hack/verify-gencode.sh  # Verify generated code is up to date
   ```

4. **Synchronize volumebinding changes from k8s**:
    - Volcano maintains volumebinding separately for compatibility with lower version kubernetes's access to the volumebinding API
    - Review k8s volumebinding changes and apply necessary updates

5. **Fix compilation and adaptation issues**:
    - Address API changes, scheduling policy updates, function name changes, etc.
    - Refer to the k8s upgrade changelog for breaking changes
    - Update code to match new k8s APIs and behaviors

6. **Run tests**:
   ```bash
   # Unit tests
   make unit-test
   
   # Verify code generation
   make verify
   
   # E2E tests on specified k8s versions
   # Test on both latest k8s version and historical versions
   ```

7. **Update documentation**:
    - Update Kubernetes compatibility in README
    - Update any version-specific documentation

8. **Support kube-scheduler's new beta features**:
    - Track new features using separate issues and PRs
    - Implement support for new scheduler features as needed

### Automatic sync to [volcano-sh/apis](https://github.com/volcano-sh/apis) repository

After merging the upgrade PR to master, the `sync-apis` workflow will automatically:
1. Verify that staging APIs generated code is up to date
2. Sync changes from `staging/src/volcano.sh/apis/` to [volcano-sh/apis](https://github.com/volcano-sh/apis) repository
3. Create a PR in the apis repository with the upgraded code

**No manual intervention needed** - the sync happens automatically via GitHub Actions.

### Other issues that may arise
1. **CRD generation changes**: After adapting to the new version of k8s, changes in CRD may occur. Check whether excessive yaml will be caused when generating CRD: https://github.com/volcano-sh/volcano/pull/3347
    - Regenerate CRDs: `make manifests`
    - Verify CRD size and structure

2. **Update build tools**: Try to update the versions of related tools such as kind and controller-gen to adapt to the new version of k8s to ensure that the generation of CRD and the verification cluster of CI runtime are correct: https://github.com/volcano-sh/volcano/pull/3404
    - Update `controller-gen` version if needed
    - Update `kind` version in CI workflows
    - Update other code generation tools



## Upgrade Workflow Summary

### Step-by-step process:

1. **Create upgrade branch**:
   ```bash
   git checkout -b upgrade-k8s-v1.XX
   ```

2. **Upgrade staging APIs** (in the same branch):
    - Update `staging/src/volcano.sh/apis/go.mod`
    - Regenerate code: `cd staging/src/volcano.sh/apis && bash hack/update-codegen.sh`
    - Test: `go build ./... && go test ./...`

3. **Upgrade main repository** (in the same branch):
    - Update `go.mod` (main and all subdirectories)
    - Regenerate code: `bash hack/update-gencode.sh`
    - Fix compilation issues
    - Update build tools and Dockerfiles

   > **Note**: Steps 2 and 3 are done sequentially in the same branch. All changes will be included in a single PR.

4. **Run all verifications**:
   ```bash
   make verify          # Code generation
   make lint           # Linting
   make lint-licenses  # License checks
   make unit-test      # Unit tests
   ```

5. **Create PR**:
    - Include all changes in a single PR
    - CI will automatically verify code generation for both main and staging
    - Get review and approval

6. **Merge to master**:
    - After merge, `sync-apis` workflow automatically syncs to [volcano-sh/apis](https://github.com/volcano-sh/apis)
    - No manual steps needed for apis repository