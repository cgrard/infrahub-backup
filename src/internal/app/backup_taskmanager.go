package app

import (
	"fmt"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (iops *InfrahubOps) backupTaskManagerDB(backupDir string) error {
	logrus.Info("Backing up PostgreSQL database...")

	// Determine writable temp directory
	tempDir := iops.getWritableTempDir("task-manager-db")
	dumpFile := tempDir + "/infrahubops_prefect.dump"

	// Create dump
	opts := &ExecOptions{Env: map[string]string{
		"PGPASSWORD": iops.config.PostgresPassword,
	}}
	if output, err := iops.Exec(
		"task-manager-db",
		[]string{"pg_dump", "-Fc", "-h", "localhost", "-U", iops.config.PostgresUsername, "-d", iops.config.PostgresDatabase, "-f", dumpFile},
		opts,
	); err != nil {
		return fmt.Errorf("failed to create postgresql dump: %w\nOutput: %v", err, output)
	}
	defer func() {
		if _, err := iops.Exec("task-manager-db", []string{"rm", dumpFile}, nil); err != nil {
			logrus.Warnf("Failed to remove temporary postgres dump: %v", err)
		}
	}()

	// Copy dump
	if err := iops.CopyFrom("task-manager-db", dumpFile, filepath.Join(backupDir, "prefect.dump")); err != nil {
		return fmt.Errorf("failed to copy postgresql dump: %w", err)
	}

	logrus.Info("PostgreSQL backup completed")
	return nil
}

func (iops *InfrahubOps) restorePostgreSQL(workDir string) error {
	logrus.Info("Restoring PostgreSQL database...")

	// Start task-manager-db
	if err := iops.StartServices("task-manager-db"); err != nil {
		return fmt.Errorf("failed to start task-manager-db: %w", err)
	}

	// Determine writable temp directory
	tempDir := iops.getWritableTempDir("task-manager-db")
	dumpFile := tempDir + "/infrahubops_prefect.dump"

	// Copy dump to container
	dumpPath := filepath.Join(workDir, "backup", "prefect.dump")
	if err := iops.CopyTo("task-manager-db", dumpPath, dumpFile); err != nil {
		return fmt.Errorf("failed to copy dump to container: %w", err)
	}
	defer func() {
		if _, err := iops.Exec("task-manager-db", []string{"rm", dumpFile}, nil); err != nil {
			logrus.Warnf("Failed to remove temporary postgres dump: %v", err)
		}
	}()

	// Restore database
	opts := &ExecOptions{Env: map[string]string{
		"PGPASSWORD": iops.config.PostgresPassword,
	}}
	if output, err := iops.Exec(
		"task-manager-db",
		// "-x", "--no-owner" for role does not exist
		[]string{"pg_restore", "-h", "localhost", "-d", "postgres", "-U", iops.config.PostgresUsername, "--clean", "--create", dumpFile},
		opts,
	); err != nil {
		return fmt.Errorf("failed to restore postgresql: %w\nOutput: %v", err, output)
	}

	return nil
}
