package cmd

import (
	"fmt"

	"github.com/libops/sitectl/pkg/docker"
	"github.com/spf13/cobra"
)

var drushCmd = &cobra.Command{
	Use:                "drush [COMMAND]",
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	Short:              "Run drush commands inside the Drupal container",
	Long: `Run drush commands inside the Drupal container of the active context.

This wraps "docker compose exec drupal drush" and automatically injects DRUPAL_DRUSH_URI
so --uri does not need to be specified manually.

Examples:
  sitectl isle drush status                 # Check Drupal status
  sitectl isle drush cr                     # Clear all caches
  sitectl isle drush cex                    # Export configuration
  sitectl isle drush cim                    # Import configuration
  sitectl isle drush sqlq "SHOW TABLES"     # Run a SQL query
  sitectl isle drush --context prod status  # Check status on the prod context`,
	RunE: func(cmd *cobra.Command, args []string) error {
		filteredArgs, ctx, cli, containerName, err := getDrupalContainer(cmd, args)
		if err != nil {
			return err
		}
		defer cli.Close()

		drushArgs := append([]string{"drush"}, filteredArgs...)
		exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
			Container:    containerName,
			Cmd:          drushArgs,
			WorkingDir:   ctx.EffectiveDrupalContainerRoot(),
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
		})
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d", exitCode)
		}

		return nil
	},
}
