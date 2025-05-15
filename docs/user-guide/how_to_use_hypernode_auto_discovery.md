# Usage Document

## Introduction

This design document describes the design and implementation of the HyperNode network topology discovery feature in Volcano. This feature automatically discovers the network topology structure within the cluster and creates and maintains HyperNode custom resources (CRs) based on the discovered topology information. Consequently, the Volcano Scheduler will leverage these HyperNode CRs for scheduling decisions, eliminating the need for users to manually maintain HyperNode information.

## Prerequisites

Please follow [this guide](https://volcano.sh/en/docs/v1-11-0/network_topology_aware_scheduling/#installing-volcano) to install Volcano with Network Topology Aware Scheduling feature enabled.

## Configuration

The HyperNode network topology discovery feature is configured via a ConfigMap. The ConfigMap contains the configuration for the discovery sources, such as UFM, RoCE, and label, you can modify the configuration according to your own cluster environments.
Please note that you should replace with your Volcano namespace if Volcano is not installed in the default namespace.

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
        config:
          endpoint: https://ufm-server:8080
          username: admin
          password: password
          insecureSkipVerify: true
      - source: roce
        enabled: false
        interval: 15m
        config:
          endpoint: https://roce-server:9090
          token: token
      - source: label
        enabled: false
        config: {}
```

### Configuration Options

*   `source`: The discovery source. Supported values are `ufm`, `roce`, and `label`.
*   `enabled`: Whether the discovery source is enabled.
*   `interval`: The interval between discovery operations. If not specified, the default value is 1 hour.
*   `config`: The configuration for the discovery source. The configuration options vary depending on the discovery source.

#### UFM Configuration Options

*   `endpoint`: The UFM API endpoint.
*   `username`: The UFM username.
*   `password`: The UFM password.
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

*   Use appropriate TLS certificates in production environments.
*   Set a reasonable discovery interval to avoid overloading the network topology data source.
*   Monitor the Volcano controller logs for errors.
