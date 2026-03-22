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
	Short:              "Run drush commands on ISLE contexts",
	Long: `Run drush commands on ISLE contexts.

This is a shorthand for "sitectl compose exec drupal drush" with automatic --uri handling.
The DRUPAL_DRUSH_URI environment variable is automatically passed unless you specify --uri or -l.

Special subcommands:
  uli - Generate and auto-open a one-time login link in your browser

Examples:
  sitectl drush status                      # Check Drupal status
  sitectl drush cr                          # Clear all caches
  sitectl drush cex                         # Export configuration
  sitectl drush cim                         # Import configuration
  sitectl drush uli                         # Generate login link and open in browser
  sitectl drush uli --uid=2                 # Login link for user ID 2
  sitectl drush sqlq "SHOW TABLES"          # Run SQL query
  sitectl drush --context prod status       # Check status on prod context`,
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
