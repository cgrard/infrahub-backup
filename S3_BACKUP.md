# S3 Backup Integration

The `infrahub-backup` tool now supports automatic upload of backups to S3-compatible storage (AWS S3, MinIO, etc.).

## Configuration

S3 upload is configured via environment variables:

| Environment Variable | Required | Description | Default |
|---------------------|----------|-------------|---------|
| `S3_BUCKET` | Yes | S3 bucket name | - |
| `S3_ACCESS_KEY_ID` | Yes | S3 access key ID | - |
| `S3_SECRET_ACCESS_KEY` | Yes | S3 secret access key | - |
| `S3_ENDPOINT` | No | Custom S3 endpoint (for MinIO, etc.) | - |
| `S3_REGION` | No | AWS region | `us-east-1` |

## Usage

### Basic usage with S3 upload

```bash
export S3_BUCKET="my-infrahub-backups"
export S3_ACCESS_KEY_ID="your-access-key"
export S3_SECRET_ACCESS_KEY="your-secret-key"

infrahub-backup create --s3-upload
```

### With custom S3-compatible endpoint (MinIO)

```bash
export S3_BUCKET="infrahub-backups"
export S3_ENDPOINT="https://minio.example.com"
export S3_ACCESS_KEY_ID="minioadmin"
export S3_SECRET_ACCESS_KEY="minioadmin"
export S3_REGION="us-east-1"

infrahub-backup create --s3-upload
```

### Combined with other options

```bash
# Force backup without checking for running tasks
infrahub-backup create --force --s3-upload

# Exclude task manager database
infrahub-backup create --exclude-task-manager --s3-upload
```

## Behavior

1. The backup is created locally in the `backup-dir` directory (default: `./infrahub_backups`)
2. After successful creation, the backup is uploaded to S3
3. The local backup file is kept (not deleted after upload)
4. If S3 upload fails, an error is returned but the local backup remains valid

## Error Handling

If S3 upload is enabled but configuration is incomplete:

```text
Error: S3 bucket not configured (set S3_BUCKET environment variable)
```

If the backup succeeds but S3 upload fails:

```text
Error: backup created but failed to upload to S3: <error details>
```

The local backup file will still be available at the path shown in the logs.

## S3 Object Details

- **Object Key**: The filename (e.g., `infrahub_backup_20260112_123045.tar.gz`)
- **Content-Type**: `application/gzip`
- **Storage**: Standard storage class

## Compatibility

The tool automatically configures compatibility mode when using a custom S3 endpoint (non-AWS). The following environment variables are set automatically:

- `AWS_S3_DISABLE_CONTENT_MD5_VALIDATION=true`
- `AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS=true`
- `AWS_S3_US_EAST_1_REGIONAL_ENDPOINT=regional`
- `AWS_S3_USE_ARN_REGION=true`
- `AWS_REQUEST_CHECKSUM_CALCULATION=when_required`
- `AWS_RESPONSE_CHECKSUM_VALIDATION=when_required`

These settings ensure compatibility with S3-compatible services that don't support all AWS S3 features.

**Tested with:**

- AWS S3
- MinIO
- DigitalOcean Spaces
- Wasabi
- Other S3-compatible storage systems supporting AWS SDK v2
