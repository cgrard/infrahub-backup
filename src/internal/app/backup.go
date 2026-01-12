package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// CreateBackup creates a full backup of the Infrahub deployment
func (iops *InfrahubOps) CreateBackup(force bool, neo4jMetadata string, excludeTaskManager bool) (retErr error) {
	if err := iops.checkPrerequisites(); err != nil {
		return err
	}

	if err := iops.DetectEnvironment(); err != nil {
		return err
	}

	edition, editionErr := iops.detectNeo4jEdition()
	if editionErr != nil {
		logrus.Warnf("Could not determine Neo4j edition: %v", editionErr)
	} else {
		logrus.Infof("Detected Neo4j %s edition", edition)
	}

	isCommunityEdition := strings.EqualFold(edition, neo4jEditionCommunity)
	if isCommunityEdition {
		logrus.Warn("Neo4j Community Edition detected; Infrahub services will be stopped and restarted before the backup begins.")
		logrus.Warn("Waiting 10 seconds to allow the user to abort... CTRL+C to cancel.")
		time.Sleep(10 * time.Second)
	}

	version := iops.getInfrahubVersion()

	// Check for running tasks unless --force is set
	if !force {
		logrus.Info("Checking for running tasks before backup...")
		if err := iops.waitForRunningTasks(); err != nil {
			return err
		}
	}

	var servicesToRestart []string
	if isCommunityEdition {
		stoppedServices, stopErr := iops.stopAppContainers()
		if stopErr != nil {
			if len(stoppedServices) > 0 {
				if startErr := iops.startAppContainers(stoppedServices); startErr != nil {
					logrus.Warnf("Failed to restart services after stop error: %v", startErr)
				}
			}
			return fmt.Errorf("failed to stop services for Neo4j Community backup: %w", stopErr)
		}
		servicesToRestart = append([]string(nil), stoppedServices...)
		defer func() {
			if len(servicesToRestart) == 0 {
				return
			}
			if startErr := iops.startAppContainers(servicesToRestart); startErr != nil {
				logrus.Errorf("Failed to restart services after backup: %v", startErr)
				if retErr == nil {
					retErr = fmt.Errorf("failed to restart services after backup: %w", startErr)
				}
			}
		}()
	}

	backupFilename := iops.generateBackupFilename()
	backupPath := filepath.Join(iops.config.BackupDir, backupFilename)
	workDir, err := os.MkdirTemp("", "infrahub_backup_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	logrus.Infof("Creating backup: %s", backupFilename)

	// Create backup directory structure
	backupDir := filepath.Join(workDir, "backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	if err := os.MkdirAll(iops.config.BackupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup parent directory: %w", err)
	}

	// Create metadata
	backupID := strings.TrimSuffix(backupFilename, ".tar.gz")
	metadata := iops.createBackupMetadata(backupID, !excludeTaskManager, version, edition)

	// Backup databases
	if err := iops.backupDatabase(backupDir, neo4jMetadata, edition); err != nil {
		return err
	}

	if !excludeTaskManager {
		if err := iops.backupTaskManagerDB(backupDir); err != nil {
			return err
		}
	} else {
		logrus.Info("Skipping task manager database backup as requested")
	}

	// Calculate checksums for backup files
	checksums := make(map[string]string)
	neo4jDir := filepath.Join(backupDir, "database")
	prefectPath := filepath.Join(backupDir, "prefect.dump")

	// Calculate checksum for each file in Neo4j backup directory
	err = filepath.Walk(neo4jDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(backupDir, path)
			if sum, err := calculateSHA256(path); err == nil {
				checksums[rel] = sum
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to calculate Neo4j backup checksums: %w", err)
	}

	// Calculate checksum for Prefect DB dump
	if !excludeTaskManager {
		if _, err := os.Stat(prefectPath); err == nil {
			if sum, err := calculateSHA256(prefectPath); err == nil {
				checksums["prefect.dump"] = sum
			} else {
				return fmt.Errorf("failed to calculate Prefect DB checksum: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not access Prefect DB dump: %w", err)
		}
	}

	if len(checksums) > 0 {
		metadata.Checksums = checksums
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(filepath.Join(backupDir, "backup_information.json"), metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// TODO: Backup artifact store
	logrus.Info("Artifact store backup will be added in future versions")

	// Create tarball
	logrus.Info("Creating backup archive...")
	if err := createTarball(backupPath, workDir, "backup/"); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	logrus.Infof("Backup created: %s", backupPath)

	// Show backup size
	if stat, err := os.Stat(backupPath); err == nil {
		logrus.Infof("Backup size: %s", formatBytes(stat.Size()))
	}

	return retErr
}

// RestoreBackup restores an Infrahub deployment from a backup archive
func (iops *InfrahubOps) RestoreBackup(backupFile string, excludeTaskManager bool, restoreMigrateFormat bool) error {
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupFile)
	}

	if err := iops.checkPrerequisites(); err != nil {
		return err
	}

	if err := iops.DetectEnvironment(); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "infrahub_restore_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	logrus.Infof("Restoring from backup: %s", backupFile)

	// Extract backup
	logrus.Info("Extracting backup archive...")
	if err := extractTarball(backupFile, workDir); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	// Validate backup
	metadataPath := filepath.Join(workDir, "backup", "backup_information.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fmt.Errorf("invalid backup file: missing metadata")
	}

	// Read and parse backup info
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}
	var metadata BackupMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	logrus.Info("Backup metadata:")
	fmt.Println(string(metadataBytes))

	neo4jEdition := strings.ToLower(metadata.Neo4jEdition)
	if detectedEdition, err := iops.detectNeo4jEdition(); err != nil {
		logrus.Warnf("Could not detect Neo4j edition during restore; defaulting to community workflow: %v", err)
		neo4jEdition = neo4jEditionCommunity
	} else {
		if neo4jEdition == neo4jEditionCommunity && strings.ToLower(detectedEdition) == neo4jEditionEnterprise {
			// if the backup artifact is a community one, always use the community method to restore
			neo4jEdition = neo4jEditionCommunity
		} else if neo4jEdition == neo4jEditionEnterprise && strings.ToLower(detectedEdition) == neo4jEditionCommunity {
			return fmt.Errorf("cannot restore Enterprise backup on Community edition Neo4j")
		} else {
			neo4jEdition = strings.ToLower(detectedEdition)
		}
		logrus.Infof("Detected Neo4j %s edition for restore", neo4jEdition)
	}

	// Determine task manager database availability
	taskManagerIncluded := false
	for _, component := range metadata.Components {
		if component == "task-manager-db" {
			taskManagerIncluded = true
			break
		}
	}
	if !taskManagerIncluded {
		if _, ok := metadata.Checksums["prefect.dump"]; ok {
			taskManagerIncluded = true
		}
	}

	// Validate checksums for Neo4j backup files
	for relPath, expectedSum := range metadata.Checksums {
		if relPath == "prefect.dump" {
			continue
		}
		filePath := filepath.Join(workDir, "backup", relPath)
		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("missing backup file: %s", relPath)
		}
		sum, err := calculateSHA256(filePath)
		if err != nil {
			return fmt.Errorf("failed to calculate checksum for %s: %w", relPath, err)
		}
		if sum != expectedSum {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", relPath, expectedSum, sum)
		}
	}

	// Validate checksum for Prefect DB dump when applicable
	prefectPath := filepath.Join(workDir, "backup", "prefect.dump")
	prefectExists := false
	if _, err := os.Stat(prefectPath); err == nil {
		prefectExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to access prefect.dump: %w", err)
	}

	shouldRestoreTaskManager := taskManagerIncluded && !excludeTaskManager
	validatePrefect := shouldRestoreTaskManager && prefectExists

	if taskManagerIncluded && !prefectExists && !excludeTaskManager {
		return fmt.Errorf("backup metadata includes task manager database but prefect.dump is missing")
	}

	if taskManagerIncluded && excludeTaskManager {
		logrus.Info("Skipping task manager database restore as requested")
	} else if !taskManagerIncluded {
		logrus.Info("Backup does not include task manager database; skipping restore")
	} else if prefectExists {
		logrus.Info("Task manager database dump detected; will restore")
	}

	if validatePrefect {
		expectedSum, ok := metadata.Checksums["prefect.dump"]
		if !ok {
			return fmt.Errorf("missing checksum for prefect.dump in metadata")
		}
		sum, err := calculateSHA256(prefectPath)
		if err != nil {
			return fmt.Errorf("failed to calculate checksum for prefect.dump: %w", err)
		}
		if sum != expectedSum {
			return fmt.Errorf("checksum mismatch for prefect.dump: expected %s, got %s", expectedSum, sum)
		}
	}

	// Wipe transient data
	iops.wipeTransientData()

	// Stop application containers
	if _, err := iops.stopAppContainers(); err != nil {
		return err
	}

	// Restore PostgreSQL when available
	if validatePrefect {
		if err := iops.restorePostgreSQL(workDir); err != nil {
			return err
		}
	} else {
		logrus.Info("Skipping task manager database restore step")
	}

	// Restart dependencies
	if err := iops.restartDependencies(); err != nil {
		return err
	}

	// Restore Neo4j
	if err := iops.restoreNeo4j(workDir, neo4jEdition, restoreMigrateFormat); err != nil {
		return err
	}

	// Restart all services
	logrus.Info("Restarting Infrahub services...")
	if err := iops.StartServices("infrahub-server", "task-worker"); err != nil {
		return fmt.Errorf("failed to restart infrahub services: %w", err)
	}

	logrus.Info("Restore completed successfully")
	logrus.Info("Infrahub should be available shortly")

	return nil
}
