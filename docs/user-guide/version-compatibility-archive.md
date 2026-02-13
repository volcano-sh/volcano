# Kubernetes Compatibility Archive

> **Note**: This page contains the complete compatibility history for all Volcano versions.  
> For the latest versions, see the [main README](../../README.md#kubernetes-compatibility).

## Complete Compatibility Matrix

|                       | Kubernetes 1.34 | Kubernetes 1.33 | Kubernetes 1.32 | Kubernetes 1.31 | Kubernetes 1.30 | Kubernetes 1.29 | Kubernetes 1.28 | Kubernetes 1.27 | Kubernetes 1.26 | Kubernetes 1.25 | Kubernetes 1.24 | Kubernetes 1.23 | Kubernetes 1.22 | Kubernetes 1.21 | Kubernetes 1.20 | Kubernetes 1.19 | Kubernetes 1.18 | Kubernetes 1.17 |
|-----------------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|-----------------|
| Volcano HEAD (master) | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               | -               | -               |
| Volcano v1.14         | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               | -               | -               |
| Volcano v1.13         | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               | -               | -               |
| Volcano v1.12         | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               |
| Volcano v1.11         | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               |
| Volcano v1.10         | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               |
| Volcano v1.9          | -               | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               | -               | -               |
| Volcano v1.8          | -               | -               | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               |
| Volcano v1.7          | -               | -               | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | -               | -               |
| Volcano v1.6          | -               | -               | -               | -               | -               | -               | -               | -               | -               | -               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               | ✓               |

**Key:**
* `✓` Volcano and the Kubernetes version are exactly compatible.
* `+` Volcano has features or API objects that may not be present in the Kubernetes version.
* `-` The Kubernetes version has features or API objects that Volcano can't use.
