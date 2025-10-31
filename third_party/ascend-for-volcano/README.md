# Ascend for Volcano

This directory contains code vendored from the `mind-cluster` project to enable Ascend vNPU features within Volcano.

## Purpose

This code is a third-party dependency required for the Ascend vNPU (virtual NPU) scheduling feature, which is a hardware-related functionality.

## Source

The code was copied from the official `mind-cluster` repository.

- **Repository**: `https://gitcode.com/Ascend/mind-cluster`
- **Current Version (Tag)**: `v7.2.RC1`
- **Copied Directories**:
    - `component/ascend-for-volcano/common`
    - `component/ascend-for-volcano/test`

## Modifications

To integrate with the Volcano project structure, the following modifications were made to the import paths in the copied source files:

- The original import path `volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util` was changed to `volcano.sh/volcano/third_party/ascend-for-volcano/common/util`.
- The original import path `volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test` was changed to `volcano.sh/volcano/third_party/ascend-for-volcano/test`. 
- The original import path `k8s.io/klog` was changed to `k8s.io/klog/v2`
- `nodeInf.Capability` in `third_party/ascend-for-volcano/test/node.go` was changed to `nodeInf.Capacity`
- `VolumeBinder:  &util.FakeVolumeBinder{}` in `third_party/ascend-for-volcano/test/frame.go` was removed
- `binder := &util.FakeBinder` in `third_party/ascend-for-volcano/test/frame.go` was changed to `binder := util.NewFakeBinder(0)`
- New Function `GetDeviceInfoAndSetInformerStart` is added to `third_party/ascend-for-volcano/common/k8s/cmmgr.go`.
- New Function `GetNodeDInfo` is added to `third_party/ascend-for-volcano/common/k8s/cmmgr.go`.
- `plugin/type.go` only reserves the top 90 lines and removes the others.

## Upgrade Process

Since this code is maintained as a manual copy, there is no automated upgrade path. To upgrade to a newer version from the upstream `mind-cluster` repository (e.g., from `v7.1.RC1` to `v7.2.RC1`), choose one of the methods below based on the expected volume of changes.

### Method 1: Applying Diffs

This method is suitable for minor upgrades where changes are expected to be small and easily reviewable. It allows for a clear audit of all modifications.

1.  **Clone the source repository** (if you haven't already):
    ```bash
    git clone https://gitcode.com/Ascend/mind-cluster.git
    cd mind-cluster
    ```
    If you already have it cloned, ensure it's up-to-date by running `git fetch --all --tags`.

2.  **Generate the diff**. This command will show all changes within the `common` and `test` directories between the old and new tags. Replace `<OLD_TAG>` and `<NEW_TAG>` with the actual versions.
    ```bash
    # Example: Comparing v7.1.RC1 with v7.2.RC1
    git diff v7.1.RC1..v7.2.RC1 -- component/ascend-for-volcano/common component/ascend-for-volcano/test
    ```

3.  **Manually apply the changes**. Review the output from the `diff` command and apply the same modifications to the code in this `volcano` project directory.

### Method 2: Delete and Replace

This method is simpler and safer for major upgrades or when changes are extensive, as it avoids potential errors from manually applying a large number of patches.

1.  **Delete**: In this `volcano` project, remove the current `common` and `test` directories under `third_party/ascend-for-volcano`.
2.  **Copy**: From a local clone of the `mind-cluster` repository (checked out at the new tag), copy the `component/ascend-for-volcano/common` and `component/ascend-for-volcano/test` directories here.
3.  **Apply Modifications**: **Crucially**, you must re-apply the import path modifications as described in the "Modifications" section above. This step is mandatory for the code to compile.

**Notice**: After upgrading using either method, always document the new version tag in this `README.md` and in the corresponding commit message.

## License

This code is subject to the license of the original project (`mind-cluster`), which is Apache License 2.0. A copy of the license should be included in this directory.
