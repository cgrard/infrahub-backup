package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

// uploadBackupToS3 uploads a backup file to S3
func (iops *InfrahubOps) uploadBackupToS3(backupPath string) error {
	if !iops.config.S3Upload {
		return nil
	}

	if err := iops.validateS3Config(); err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"bucket":   iops.config.S3Bucket,
		"endpoint": iops.config.S3Endpoint,
		"region":   iops.config.S3Region,
	}).Info("Uploading backup to S3...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	s3Client, err := iops.createS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	filename := filepath.Base(backupPath)
	key := filename

	logrus.WithFields(logrus.Fields{
		"file": filename,
		"size": formatBytes(stat.Size()),
		"key":  key,
	}).Info("Starting S3 upload...")

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(iops.config.S3Bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(stat.Size()),
		ContentType:   aws.String("application/gzip"),
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bucket": iops.config.S3Bucket,
		"key":    key,
		"size":   formatBytes(stat.Size()),
	}).Info("Backup successfully uploaded to S3")

	return nil
}

// validateS3Config validates that all required S3 configuration is present
func (iops *InfrahubOps) validateS3Config() error {
	if iops.config.S3Bucket == "" {
		return fmt.Errorf("S3 bucket not configured (set S3_BUCKET environment variable)")
	}
	if iops.config.S3AccessKeyID == "" {
		return fmt.Errorf("S3 access key ID not configured (set S3_ACCESS_KEY_ID environment variable)")
	}
	if iops.config.S3SecretKey == "" {
		return fmt.Errorf("S3 secret key not configured (set S3_SECRET_ACCESS_KEY environment variable)")
	}
	return nil
}

// createS3Client creates an S3 client with the configured credentials
func (iops *InfrahubOps) createS3Client(ctx context.Context) (*s3.Client, error) {
	// Configure environment variables for S3-compatible services (non-AWS endpoints)
	if iops.config.S3Endpoint != "" {
		iops.configureS3CompatibilityMode()
	}

	credProvider := credentials.NewStaticCredentialsProvider(
		iops.config.S3AccessKeyID,
		iops.config.S3SecretKey,
		"",
	)

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(iops.config.S3Region),
		config.WithCredentialsProvider(credProvider),
	)
	if err != nil {
		return nil, err
	}

	var options []func(*s3.Options)
	if iops.config.S3Endpoint != "" {
		options = append(options, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(iops.config.S3Endpoint)
			o.UsePathStyle = true // Required for MinIO and some S3-compatible services
		})
	}

	return s3.NewFromConfig(cfg, options...), nil
}

// configureS3CompatibilityMode sets environment variables for S3-compatible services
func (iops *InfrahubOps) configureS3CompatibilityMode() {
	logrus.Debug("Configuring S3 compatibility mode for non-AWS endpoint")

	// Disable features not supported by all S3-compatible services
	envVars := map[string]string{
		"AWS_S3_DISABLE_CONTENT_MD5_VALIDATION":       "true",
		"AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS":    "true",
		"AWS_S3_US_EAST_1_REGIONAL_ENDPOINT":          "regional",
		"AWS_S3_USE_ARN_REGION":                       "true",
		"AWS_REQUEST_CHECKSUM_CALCULATION":            "when_required",
		"AWS_RESPONSE_CHECKSUM_VALIDATION":            "when_required",
	}

	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			logrus.Warnf("Failed to set %s: %v", key, err)
		}
	}
}
