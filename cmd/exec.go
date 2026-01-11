package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:                "exec [COMMAND]",
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	Short:              "Execute commands in the Drupal container",
	Long: `Execute arbitrary commands in the Drupal container.

If no command is provided, opens an interactive bash shell in the container.
This is useful for debugging, running composer commands, or performing file operations.

Examples:
  sitectl drupal exec                              # Open interactive bash shell
  sitectl drupal exec ls -la /var/www/drupal/web   # List files
  sitectl drupal exec composer require drupal/devel # Install a module
  sitectl drupal exec "drush cr && drush status"   # Run multiple commands`,
	RunE: func(cmd *cobra.Command, args []string) error {
		filteredArgs, _, cli, containerName, err := getDrupalContainer(cmd, args)
		if err != nil {
			return err
		}
		defer cli.Close()

		// Default to bash if no command specified
		execCmd := filteredArgs
		if len(execCmd) == 0 {
			execCmd = []string{"bash"}
		}

		// Execute the command interactively using SDK helper
		exitCode, err := sdk.ExecInContainerInteractive(context.Background(), containerName, execCmd)
		if err != nil {
			return err
		}

		// Exit with the same code as the container command
		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d", exitCode)
		}

		return nil
	},
}
