package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	pluginjobs "github.com/libops/sitectl-drupal/pkg/jobs"
	"github.com/libops/sitectl/pkg/config"
	corejob "github.com/libops/sitectl/pkg/job"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	syncSourceContext string
	syncTargetContext string
	syncDrupalRootfs  string
	syncFresh         bool
	syncBackupDir     string
	syncYolo          bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Drupal artifacts between contexts",
}

var syncDatabaseCmd = &cobra.Command{
	Use:     "database",
	Aliases: []string{"db"},
	Short:   "Sync the Drupal database from one context to another",
	RunE: func(cmd *cobra.Command, args []string) error {
		progress := plugin.NewProgressLine(cmd.ErrOrStderr(), "Syncing Drupal Database", "Resolving contexts")
		defer progress.Close()

		sourceCtx, targetCtx, err := corejob.ResolveContextPair(syncSourceContext, syncTargetContext)
		if err != nil {
			return err
		}

		workDir, cleanupWorkDir, err := corejob.MakeTempWorkDir("sitectl-drupal-sync-db-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer cleanupWorkDir()

		progress.Report("Syncing Drupal Database", fmt.Sprintf("Resolving source artifact from %s", sourceCtx.Name))
		sourceArtifactPath, err := resolveSourceDBArtifact(cmd, sourceCtx)
		if err != nil {
			return err
		}

		progress.Report("Syncing Drupal Database", fmt.Sprintf("Staging artifact from %s to %s", sourceCtx.Name, targetCtx.Name))
		targetHostPath, cleanupTarget, err := corejob.StageArtifactBetweenContexts(
			cmd.Context(),
			sourceCtx,
			targetCtx,
			sourceArtifactPath,
			workDir,
			"drupal.sql.gz",
			"sitectl-drupal-sync",
		)
		if err != nil {
			return fmt.Errorf("download database artifact from %q: %w", sourceCtx.Name, err)
		}
		defer cleanupTarget()

		progress.Report("Syncing Drupal Database", fmt.Sprintf("Importing into %s", targetCtx.Name))
		if !syncYolo {
			progress.Close()
		}
		if err := pluginjobs.RunDBImport(cmd, targetCtx, targetHostPath, syncYolo); err != nil {
			return fmt.Errorf("import database into %q: %w", targetCtx.Name, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Database synced from %s to %s\n", sourceCtx.Name, targetCtx.Name)
		return nil
	},
}

var syncConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Sync the Drupal config/sync directory from one context to another",
	RunE: func(cmd *cobra.Command, args []string) error {
		progress := plugin.NewProgressLine(cmd.ErrOrStderr(), "Syncing Drupal Config", "Resolving contexts")
		defer progress.Close()

		sourceCtx, targetCtx, err := corejob.ResolveContextPair(syncSourceContext, syncTargetContext)
		if err != nil {
			return err
		}

		workDir, cleanupWorkDir, err := corejob.MakeTempWorkDir("sitectl-drupal-sync-config-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer cleanupWorkDir()

		artifactName := corejob.SyncArtifactName("sitectl-drupal-sync", "config.tar.gz")
		sourceHostPath := filepath.ToSlash(filepath.Join("/tmp", artifactName))

		progress.Report("Syncing Drupal Config", fmt.Sprintf("Exporting config from %s", sourceCtx.Name))
		if err := pluginjobs.RunConfigExport(cmd, sourceCtx, sourceHostPath); err != nil {
			return fmt.Errorf("export config from %q: %w", sourceCtx.Name, err)
		}
		defer corejob.RemoveContextHostPath(cmd.Context(), sourceCtx, sourceHostPath)

		progress.Report("Syncing Drupal Config", fmt.Sprintf("Staging artifact from %s to %s", sourceCtx.Name, targetCtx.Name))
		targetHostPath, cleanupTarget, err := corejob.StageArtifactBetweenContexts(
			cmd.Context(),
			sourceCtx,
			targetCtx,
			sourceHostPath,
			workDir,
			"config.tar.gz",
			"sitectl-drupal-sync",
		)
		if err != nil {
			return fmt.Errorf("stage config artifact from %q to %q: %w", sourceCtx.Name, targetCtx.Name, err)
		}
		defer cleanupTarget()

		progress.Report("Syncing Drupal Config", fmt.Sprintf("Importing into %s", targetCtx.Name))
		if err := pluginjobs.RunConfigImport(cmd, targetCtx, targetHostPath, syncDrupalRootfs); err != nil {
			return fmt.Errorf("import config into %q: %w", targetCtx.Name, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config synced from %s to %s\n", sourceCtx.Name, targetCtx.Name)
		return nil
	},
}

func init() {
	syncDatabaseCmd.Flags().StringVar(&syncSourceContext, "source", "", "Source sitectl context")
	syncDatabaseCmd.Flags().StringVar(&syncTargetContext, "target", "", "Target sitectl context")
	syncDatabaseCmd.Flags().BoolVar(&syncFresh, "fresh", false, "Always run a fresh source database backup instead of reusing today/yesterday if available")
	syncDatabaseCmd.Flags().StringVar(&syncBackupDir, "backup-dir", "/tmp/sitectl-drupal-jobs/db-backup", "Source host directory used to cache database backup artifacts for sync")
	syncDatabaseCmd.Flags().BoolVar(&syncYolo, "yolo", false, "Apply destructive database changes without confirmation")
	must(syncDatabaseCmd.MarkFlagRequired("source"))
	must(syncDatabaseCmd.MarkFlagRequired("target"))

	syncConfigCmd.Flags().StringVar(&syncSourceContext, "source", "", "Source sitectl context")
	syncConfigCmd.Flags().StringVar(&syncTargetContext, "target", "", "Target sitectl context")
	syncConfigCmd.Flags().StringVar(&syncDrupalRootfs, "drupal-rootfs", "", "Drupal rootfs relative to the target context project dir")
	must(syncConfigCmd.MarkFlagRequired("source"))
	must(syncConfigCmd.MarkFlagRequired("target"))

	syncCmd.AddCommand(syncDatabaseCmd)
	syncCmd.AddCommand(syncConfigCmd)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func resolveSourceDBArtifact(cmd *cobra.Command, ctx *config.Context) (string, error) {
	return corejob.ResolveRecentArtifact(ctx, syncBackupDir, "drupal.sql.gz", syncFresh, time.Now().UTC(), func(path string) error {
		if err := pluginjobs.RunDBBackup(cmd, ctx, path); err != nil {
			return fmt.Errorf("run source db-backup job on %q: %w", ctx.Name, err)
		}
		return nil
	})
}
