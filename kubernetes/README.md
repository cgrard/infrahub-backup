# Kubernetes Deployment

This directory contains Kubernetes manifests to deploy `infrahub-backup` as a CronJob.

## Files

- **[cronjob.yaml](cronjob.yaml)** - CronJob definition that runs backups on a schedule
- **[configmap.yaml](configmap.yaml)** - Non-sensitive configuration (namespace, backup directory, log format)
- **[secret.yaml](secret.yaml)** - S3 credentials and configuration
- **[rbac.yaml](rbac.yaml)** - ServiceAccount, Role, and RoleBinding for Kubernetes permissions
- **[kustomization.yaml](kustomization.yaml)** - Kustomize configuration for easy deployment

## Prerequisites

1. **Docker Image**: Build and push the Docker image to your container registry:

   ```bash
   # Build the image
   docker build -t your-registry/infrahub-backup:latest .

   # Push to registry
   docker push your-registry/infrahub-backup:latest
   ```

2. **Infrahub Deployment**: The tool expects Infrahub to be deployed in Kubernetes with standard service names:
   - `database` (Neo4j)
   - `task-manager-db` (PostgreSQL)
   - Associated deployments/statefulsets

## Configuration

### 1. Update Secret with S3 Credentials

Edit [secret.yaml](secret.yaml) and replace the placeholder values:

```yaml
stringData:
  bucket: "infrahub-backups"
  endpoint: "https://s3.amazonaws.com"  # For AWS S3, or MinIO endpoint
  region: "us-east-1"
  access-key-id: "your-access-key-id"
  secret-access-key: "your-secret-access-key"
```

**For AWS S3**: Leave `endpoint` empty or remove it
**For MinIO/S3-compatible**: Set `endpoint` to your service URL

### 2. Update ConfigMap

Edit [configmap.yaml](configmap.yaml) to customize:

```yaml
data:
  k8s-namespace: "infrahub"  # Namespace where Infrahub is deployed
  backup-dir: "/backups"
  log-format: "json"
```

### 3. Update CronJob Schedule

Edit [cronjob.yaml](cronjob.yaml) to change the backup schedule:

```yaml
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
```

Cron schedule format:

```text
┌───────────── minute (0 - 59)
│ ┌───────────── hour (0 - 23)
│ │ ┌───────────── day of month (1 - 31)
│ │ │ ┌───────────── month (1 - 12)
│ │ │ │ ┌───────────── day of week (0 - 6) (Sunday to Saturday)
│ │ │ │ │
│ │ │ │ │
* * * * *
```

Examples:

- `0 2 * * *` - Daily at 2 AM
- `0 */6 * * *` - Every 6 hours
- `0 2 * * 0` - Weekly on Sunday at 2 AM
- `0 2 1 * *` - Monthly on the 1st at 2 AM

### 4. Update Image Reference

Edit [kustomization.yaml](kustomization.yaml) to point to your container registry:

```yaml
images:
- name: infrahub-backup
  newName: your-registry/infrahub-backup
  newTag: latest
```

## Deployment

### Using kubectl

Deploy all resources to the `infrahub` namespace:

```bash
kubectl apply -f kubernetes/
```

Or apply files individually:

```bash
kubectl apply -f kubernetes/rbac.yaml
kubectl apply -f kubernetes/configmap.yaml
kubectl apply -f kubernetes/secret.yaml
kubectl apply -f kubernetes/cronjob.yaml
```

### Using Kustomize

```bash
kubectl apply -k kubernetes/
```

## Verification

### Check CronJob Status

```bash
kubectl get cronjob -n infrahub
kubectl describe cronjob infrahub-backup -n infrahub
```

### View Job Runs

```bash
# List all jobs created by the CronJob
kubectl get jobs -n infrahub -l app=infrahub-backup

# View recent job logs
kubectl logs -n infrahub -l app=infrahub-backup --tail=100
```

### Manual Trigger

To manually trigger a backup without waiting for the schedule:

```bash
kubectl create job -n infrahub --from=cronjob/infrahub-backup infrahub-backup-manual-$(date +%s)
```

### Check Backup in S3

Verify backups are being uploaded to your S3 bucket:

```bash
# For AWS S3
aws s3 ls s3://my-infrahub-backups/

# For MinIO
mc ls myminio/my-infrahub-backups/
```

## Troubleshooting

### Check Permissions

Verify the ServiceAccount has correct RBAC permissions:

```bash
kubectl auth can-i get pods --as=system:serviceaccount:infrahub:infrahub-backup -n infrahub
kubectl auth can-i create pods/exec --as=system:serviceaccount:infrahub:infrahub-backup -n infrahub
kubectl auth can-i patch deployments --as=system:serviceaccount:infrahub:infrahub-backup -n infrahub
```

### View Job Logs

```bash
# Get the latest job
JOB_NAME=$(kubectl get jobs -n infrahub -l app=infrahub-backup --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}')

# View logs
kubectl logs -n infrahub job/$JOB_NAME
```

### Test S3 Connectivity

Create a test pod to verify S3 configuration:

```bash
kubectl run -n infrahub s3-test --rm -it --restart=Never \
  --image=amazon/aws-cli \
  --env="AWS_ACCESS_KEY_ID=your-key" \
  --env="AWS_SECRET_ACCESS_KEY=your-secret" \
  --env="AWS_DEFAULT_REGION=us-east-1" \
  -- s3 ls s3://my-infrahub-backups/
```

### Common Issues

1. **ImagePullBackOff**: Check image name in kustomization.yaml and ensure it's pushed to registry
2. **Permission Denied**: Verify RBAC permissions are correctly applied
3. **S3 Upload Failed**: Check secret values and network connectivity to S3 endpoint
4. **Backup Failed**: Check logs for specific error messages

## Resource Limits

The CronJob is configured with default resource limits:

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "100m"
  limits:
    memory: "1Gi"
    cpu: "500m"
```

Adjust these based on your backup size and database workload.

## Security Considerations

1. **Secrets Management**: Consider using a secrets manager (e.g., HashiCorp Vault, AWS Secrets Manager, Sealed Secrets)
2. **RBAC**: The ServiceAccount has minimal permissions required for backup operations
3. **Network Policies**: Consider adding network policies to restrict CronJob network access
4. **Image Security**: Scan the Docker image for vulnerabilities before deployment
5. **Non-root User**: The container runs as non-root user (uid/gid 1000)

## Backup Retention

The CronJob keeps history of:

- Last 3 successful job runs
- Last 3 failed job runs

Adjust these in [cronjob.yaml](cronjob.yaml):

```yaml
spec:
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
```

S3 bucket lifecycle policies should be configured separately for backup file retention.

## Advanced Configuration

### Exclude Task Manager Database

To skip backing up the task manager database, add the flag to the CronJob args:

```yaml
args:
- create
- --s3-upload
- --exclude-task-manager
- --k8s-namespace=$(K8S_NAMESPACE)
```

### Force Backup

To force backup even if tasks are running:

```yaml
args:
- create
- --s3-upload
- --force
- --k8s-namespace=$(K8S_NAMESPACE)
```

### Multiple Namespaces

To backup Infrahub instances in different namespaces, create separate CronJobs with different configurations.

## Monitoring

Consider adding monitoring for:

- Job completion status (success/failure)
- Backup file size
- S3 upload duration
- Storage usage in S3 bucket

Integration examples:

- Prometheus metrics via custom ServiceMonitor
- Alerting via AlertManager rules
- Logging via Fluentd/Fluent Bit to centralized logging
