# RingsConfigMap Plugin

The RingsConfigMap plugin is designed to automatically manage ConfigMaps for Ascend 910 ring configuration in Volcano jobs. It's highly configurable and supports Go template syntax for dynamic naming.

## Features

1. **Automatic ConfigMap Creation**: Creates a ConfigMap with customizable naming using Go templates
2. **Volume Mounting**: Automatically mounts the ConfigMap as a volume to all containers in the job
3. **Automatic Cleanup**: Deletes the ConfigMap when the job is deleted
4. **Idempotent Operations**: All operations are idempotent to prevent duplicate resources
5. **Highly Configurable**: Supports custom volume names, mount paths, and ConfigMap naming templates

## Usage

### Basic Usage

To use the RingsConfigMap plugin with default settings, add it to your Volcano job specification:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: my-job
  namespace: default
spec:
  minAvailable: 1
  schedulerName: volcano
  plugins:
    ringsconfigmap: {}
  tasks:
  - name: "task-1"
    replicas: 1
    template:
      spec:
        containers:
        - name: my-container
          image: my-image:latest
        restartPolicy: Never
```

### Advanced Usage with Custom Configuration

You can customize the plugin behavior using command-line arguments:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: my-job
  namespace: default
spec:
  minAvailable: 1
  schedulerName: volcano
  plugins:
    ringsconfigmap:
      - "-configmap-name-template"
      - "my-config-{{.Name}}-{{.Namespace}}"
      - "-volume-name"
      - "my-custom-volume"
      - "-mount-path"
      - "/custom/config/path"
  tasks:
  - name: "task-1"
    replicas: 1
    template:
      spec:
        containers:
        - name: my-container
          image: my-image:latest
        restartPolicy: Never
```

## Configuration Options

The plugin supports the following configuration options:

| Option | Default | Description |
|--------|---------|-------------|
| `-configmap-name-template` | `rings-config-{{.Name}}` | Go template for ConfigMap naming. Supports job properties like `{{.Name}}`, `{{.Namespace}}`, etc. |
| `-volume-name` | `ascend-910-config` | Name of the volume to be added to pods |
| `-mount-path` | `/user/serverid/devindex/config` | Path where the ConfigMap will be mounted |
| `-configmap-labels` | `{"ring-controller.atlas":"ascend-910b"}` | ConfigMap labels in JSON format |
| `-configmap-data` | `{"hccl.json":"{\"status\":\"initializing\"}"}` | ConfigMap data in JSON format |
| `-configmap-annotations` | `{}` | ConfigMap annotations in JSON format |

### Go Template Examples

The ConfigMap name template supports Go template syntax with access to job properties:

- `rings-config-{{.Name}}` → `rings-config-my-job`
- `my-config-{{.Name}}-{{.Namespace}}` → `my-config-my-job-default`
- `config-{{.Name}}-{{.UID}}` → `config-my-job-12345678-1234-1234-1234-123456789abc`

## What the Plugin Does

### On Job Add (`OnJobAdd`)

1. Creates a ConfigMap with the following specifications:
   - **Name**: Generated from the configured template (default: `rings-config-{.Name}`)
   - **Namespace**: Same as the job namespace
   - **Labels**: `ring-controller.atlas: ascend-910b` (configurable)
   - **Data**: Contains `hccl.json` with initializing status (configurable)
   - **Owner Reference**: Set to the job for automatic cleanup

### On Pod Create (`OnPodCreate`)

1. Adds a volume with configurable name (default: `ascend-910-config`) that references the ConfigMap
2. Adds a volume mount to all containers and init containers:
   - **Name**: Configurable volume name (default: `ascend-910-config`)
   - **Mount Path**: Configurable mount path (default: `/user/serverid/devindex/config`)

### On Job Delete (`OnJobDelete`)

1. Deletes the ConfigMap (name generated from template) from the job's namespace
2. Removes the plugin tracking from job status

## Generated ConfigMap Example

When a job named `my-job` is created with default settings, the plugin will create a ConfigMap like this:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: rings-config-my-job
  namespace: default
  labels:
    ring-controller.atlas: ascend-910b
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    kind: Job
    name: my-job
    uid: <job-uid>
data:
  hccl.json: |
    {
        "status":"initializing"
    }
```

## Generated Pod Volume Example

The plugin will add the following volume and volume mount to each pod (with default settings):

```yaml
spec:
  volumes:
  - name: ascend-910-config
    configMap:
      name: rings-config-my-job
  containers:
  - name: my-container
    volumeMounts:
    - name: ascend-910-config
      mountPath: /user/serverid/devindex/config
```

## Examples

### Example 1: Custom ConfigMap Naming

```yaml
plugins:
  ringsconfigmap:
    - "-configmap-name-template"
    - "my-app-config-{{.Name}}-{{.Namespace}}"
```

This will create a ConfigMap named `my-app-config-my-job-default`.

### Example 2: Custom Volume and Mount Path

```yaml
plugins:
  ringsconfigmap:
    - "-volume-name"
    - "app-config"
    - "-mount-path"
    - "/app/config"
```

This will create a volume named `app-config` mounted at `/app/config`.

### Example 3: Complete Custom Configuration

```yaml
plugins:
  ringsconfigmap:
    - "-configmap-name-template"
    - "app-{{.Name}}-config"
    - "-volume-name"
    - "app-config-volume"
    - "-mount-path"
    - "/etc/app/config"
    - "-configmap-labels"
    - '{"app":"myapp","version":"v1"}'
    - "-configmap-data"
    - '{"config.json":"{\"debug\":true}","env.yaml":"environment: production"}'
```

This will create a ConfigMap named `app-my-job-config` with custom labels and data, and mount it as volume `app-config-volume` at `/etc/app/config`.

### Example 4: Custom Labels, Data, and Annotations

```yaml
plugins:
  ringsconfigmap:
    - "-configmap-labels"
    - '{"team":"ai","project":"ascend"}'
    - "-configmap-data"
    - '{"hccl.json":"{\"status\":\"ready\",\"nodes\":4}"}'
    - "-configmap-annotations"
    - '{"version":"v1.0","created-by":"volcano"}'
```

This will use the default ConfigMap naming and volume configuration, but with custom labels, data, and annotations.

## Notes

- The plugin is idempotent, meaning it can be called multiple times without creating duplicate resources
- The ConfigMap is automatically cleaned up when the job is deleted
- The plugin only processes jobs that haven't been processed before (tracked via job status)
- All operations are safe and won't overwrite existing volumes or volume mounts
