package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ConfigureRootCommand wires shared flags, environment variables, and logging for CLI binaries.
func ConfigureRootCommand(cmd *cobra.Command, app *InfrahubOps) {
	cfg := app.Config()

	cmd.PersistentFlags().StringVar(&cfg.DockerComposeProject, "project", cfg.DockerComposeProject, "Target specific Docker Compose project")
	cmd.PersistentFlags().StringVar(&cfg.BackupDir, "backup-dir", cfg.BackupDir, "Backup directory")
	cmd.PersistentFlags().StringVar(&cfg.K8sNamespace, "k8s-namespace", cfg.K8sNamespace, "Target Kubernetes namespace")
	cmd.PersistentFlags().String("log-format", "text", "Log output format: text or json (can also set INFRAHUB_LOG_FORMAT)")
	cmd.PersistentFlags().BoolVar(&cfg.S3Upload, "s3-upload", false, "Upload backup to S3 (requires S3_* env vars)")

	bind := func(name string) {
		if err := viper.BindPFlag(name, cmd.PersistentFlags().Lookup(name)); err != nil {
			panic(err)
		}
	}

	bind("project")
	bind("backup-dir")
	bind("k8s-namespace")
	bind("log-format")
	bind("s3-upload")

	cobra.OnInitialize(func() {
		viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		viper.AutomaticEnv()
		viper.SetEnvPrefix("INFRAHUB")

		if viper.IsSet("project") {
			cfg.DockerComposeProject = viper.GetString("project")
		}
		if viper.IsSet("backup-dir") {
			cfg.BackupDir = viper.GetString("backup-dir")
		}
		if viper.IsSet("k8s-namespace") {
			cfg.K8sNamespace = viper.GetString("k8s-namespace")
		}
		if viper.IsSet("s3-upload") {
			cfg.S3Upload = viper.GetBool("s3-upload")
		}

		// Load S3 configuration from environment variables
		loadS3Config(cfg)

		switch viper.GetString("log-format") {
		case "json":
			logrus.SetFormatter(&logrus.JSONFormatter{})
		default:
			logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
		}
	})
}

// loadS3Config loads S3 configuration from environment variables
func loadS3Config(cfg *Configuration) {
	if bucket := os.Getenv("S3_BUCKET"); bucket != "" {
		cfg.S3Bucket = bucket
	}
	if endpoint := os.Getenv("S3_ENDPOINT"); endpoint != "" {
		cfg.S3Endpoint = endpoint
	}
	if accessKey := os.Getenv("S3_ACCESS_KEY_ID"); accessKey != "" {
		cfg.S3AccessKeyID = accessKey
	}
	if secretKey := os.Getenv("S3_SECRET_ACCESS_KEY"); secretKey != "" {
		cfg.S3SecretKey = secretKey
	}
	if region := os.Getenv("S3_REGION"); region != "" {
		cfg.S3Region = region
	} else {
		cfg.S3Region = "us-east-1" // Default region
	}
}

// AttachEnvironmentCommands wires the environment detection subcommands onto a root command.
func AttachEnvironmentCommands(rootCmd *cobra.Command, app *InfrahubOps) {
	envCmd := &cobra.Command{
		Use:   "environment",
		Short: "Environment detection and management",
		Long:  "Detect and list Infrahub deployment environments across Docker and Kubernetes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	detectCmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect the active deployment environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.DetectEnvironment()
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available Infrahub deployment targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			executor := NewCommandExecutor()
			dockerProjects, _ := ListDockerProjects(executor)
			k8sNamespaces, _ := ListKubernetesNamespaces(executor)

			if len(dockerProjects) == 0 && len(k8sNamespaces) == 0 {
				logrus.Info("No Infrahub deployments detected")
				return nil
			}

			if len(dockerProjects) > 0 {
				logrus.Info("Docker Compose projects:")
				for _, project := range dockerProjects {
					fmt.Printf("  %s\n", project)
				}
			}

			if len(k8sNamespaces) > 0 {
				logrus.Info("Kubernetes namespaces:")
				for _, ns := range k8sNamespaces {
					fmt.Printf("  %s\n", ns)
				}
			}

			return nil
		},
	}

	envCmd.AddCommand(detectCmd)
	envCmd.AddCommand(listCmd)
	rootCmd.AddCommand(envCmd)
}
