# Usage Document

## Introduction

This document describes how to use the HyperNode network topology auto-discovery feature in Volcano. This feature automatically discovers the network topology within the cluster and creates and maintains HyperNode custom resources (CRs) based on the discovered information. The Volcano scheduler leverages these HyperNode CRs for scheduling decisions, eliminating the need for users to manually maintain HyperNode information.

## Prerequisites

Please [Install Volcano](https://github.com/volcano-sh/volcano/tree/master?tab=readme-ov-file#quick-start-guide) with version >= v1.12.0 first.

## Configuration

The HyperNode network topology discovery feature is configured via a ConfigMap. The ConfigMap contains the configuration for the discovery sources, such as UFM, RoCE, and label, you can modify the configuration according to your own cluster environments.
Please note that you should replace with your Volcano namespace if Volcano is not installed in the default namespace.

### Secret Configuration (Required First Step)

Before configuring the UFM discovery, you must first create a Kubernetes Secret to store your UFM credentials:

```bash
kubectl create secret generic ufm-credentials \
  --from-literal=username='your-ufm-username' \
  --from-literal=password='your-ufm-password' \
  -n volcano-system
```
 > Note: Replace your-ufm-username and your-ufm-password with your actual UFM credentials, and adjust the namespace if needed.

### Example ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-controller-configmap
  namespace: volcano-system # Replace with your Volcano namespace if Volcano is not installed in the default namespace.
data:
  volcano-controller.conf: |
    networkTopologyDiscovery:
      - source: ufm
        enabled: true
        interval: 10m
        credentials:
          secretRef:
            name: ufm-credentials # Replace with the secret name that stores the UFM credentials.
            namespace: volcano-system #Replace with the secret namespace that stores the UFM credentials.
        config:
          endpoint: https://ufm-server:8080
          insecureSkipVerify: true
      - source: roce
        enabled: false
        interval: 15m
        config:
          endpoint: https://roce-server:9090
      - source: label
        enabled: false
        config: {}
```

### Configuration Options

*   `source`: The discovery source. Supported values are `ufm`, `roce`, and `label`.
*   `enabled`: Whether the discovery source is enabled.
*   `interval`: The interval between discovery operations. If not specified, the default value is 1 hour.
*   `config`: The configuration for the discovery source. The configuration options vary depending on the discovery source.
*   `credentials`: The credentials configuration for accessing the discovery source.
      * `secretRef`: Reference to a Kubernetes Secret containing credentials.
        * `name`: The name of the Secret.
        * `namespace`: The namespace of the Secret.

#### UFM Configuration Options

*   `endpoint`: The UFM API endpoint.
*   `insecureSkipVerify`: Whether to skip TLS certificate verification. This should only be used in development environments.

#### RoCE Configuration Options(Currently not supported)

*   `endpoint`: The RoCE API endpoint.
*   `token`: The RoCE API token.

#### Label Configuration Options(Currently not supported)

*   No configuration options are currently supported for the label discovery source.

## Verification

1.  Check the Volcano controller logs to ensure that the discovery sources are started successfully.

```bash
kubectl logs -n volcano-system -l app=volcano-controllers -c volcano-controllers | grep "Successfully started all network topology discoverers"
```

2.  Check the created HyperNode resources.

```bash
kubectl get hypernodes -l volcano.sh/network-topology-source=<source>
```

Replace `<source>` with the discovery source you configured, such as `ufm`.

## Troubleshooting

*   If the discovery sources are not started successfully, check the Volcano controller logs for errors.
*   If the HyperNode resources are not created, check the discovery source configuration and ensure that the discovery source is able to connect to the network topology data source.

## Best Practices

* Volcano uses Kubernetes-standard Secrets to store sensitive credential information (username/password or token). For more stringent key encryption requirements, users should consider additional mechanisms like [Encrypting Secret Data at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/).
* The credential Secrets can be placed in a specified namespace for better isolation.
* For UFM discoverer, the controller only needs read access to the specific Secret containing credentials.
* When deploying in production environments, proper RBAC policies should be configured to limit access to Secrets.
* TLS certificate verification should be enabled in production environments to prevent MITM attacks.
* Monitor the Volcano controller logs for errors.
* Set a reasonable discovery interval to avoid overloading the network topology data source.
