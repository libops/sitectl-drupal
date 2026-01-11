package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup [code|db|files]",
	Short: "Backup Drupal database",
	Long: `Create a backup of the Drupal database.

This creates a gzipped SQL dump of the database to /tmp/db.tar.gz in the container.
Cache tables are excluded from the dump for efficiency, but their structure is preserved.

Optional argument can be "code", "db", or "files" to pass as a flag to drush archive:dump.

Example:
  sitectl drupal backup              # Backup code, database, and files to /tmp/backup.tar.gz
  sitectl drupal backup code         # Backup with --code flag
  sitectl drupal backup db           # Backup with --db flag
  sitectl drupal backup files        # Backup with --files flag
  sitectl drupal backup --context prod  # Backup production database`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var extraFlag string
		destinationName := "archive"
		if len(args) > 0 {
			arg := args[0]
			if arg != "code" && arg != "db" && arg != "files" {
				return fmt.Errorf("invalid argument: must be 'code', 'db', or 'files'")
			}
			extraFlag = "--" + arg
			destinationName = arg
		}

		_, cli, containerName, err := getDrupalContainerFromFlags(cmd)
		if err != nil {
			return err
		}
		defer cli.Close()

		cmdArgs := []string{
			"drush",
			"archive:dump",
			"-y",
			"--skip-tables-list=cache,cache_*,watchdog",
			"--structure-tables-list=cache,cache_*,watchdog",
			"--debug",
			"--overwrite",
			fmt.Sprintf("--destination=/tmp/%s-%d.tar.gz", destinationName, time.Now().Unix()),
			"--exclude-code-paths=web/sites/default/settings.php",
		}
		if extraFlag != "" {
			cmdArgs = append(cmdArgs, extraFlag)
		}

		exitCode, err := sdk.ExecInContainerInteractive(context.Background(), containerName, cmdArgs)
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d", exitCode)
		}

		return nil
	},
}
