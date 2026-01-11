package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kballard/go-shellquote"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/helpers"
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
		filteredArgs, _, cli, containerName, err := getDrupalContainer(cmd, args)
		if err != nil {
			return err
		}
		defer cli.Close()

		// Build the drush command with arguments
		drushCmd := []string{"bash", "-c", fmt.Sprintf("drush %s", shellquote.Join(filteredArgs...))}

		// Execute the command interactively using SDK helper
		exitCode, err := sdk.ExecInContainerInteractive(context.Background(), containerName, drushCmd)
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d", exitCode)
		}

		return nil
	},
}

// login runs drush uli
var loginCmd = &cobra.Command{
	Use:   "uli",
	Short: "Generate a one-time login link",
	Long: `Generate a one-time login link and automatically open it in your default browser.

This runs 'drush uli' in the Drupal container and opens the resulting URL.

Examples:
  sitectl drush uli              # Login as admin (user 1)
  sitectl drush uli --uid=2      # Login as user ID 2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, cli, containerName, err := getDrupalContainerFromFlags(cmd)
		if err != nil {
			return err
		}
		defer cli.Close()

		uid, err := cmd.Flags().GetUint("uid")
		if err != nil {
			return err
		}

		// Capture output to get the URL
		var stdout, stderr bytes.Buffer
		drushCmd := []string{"bash", "-c", fmt.Sprintf("drush uli --uid=%d", uid)}

		exitCode, err := cli.Exec(context.Background(), docker.ExecOptions{
			Container:    containerName,
			Cmd:          drushCmd,
			AttachStdout: true,
			AttachStderr: true,
			Stdout:       &stdout,
			Stderr:       &stderr,
		})
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d\n%s", exitCode, stderr.String())
		}

		output := strings.TrimSpace(stdout.String())
		fmt.Println(output)

		if strings.HasPrefix(output, "http") {
			err := helpers.OpenURL(output)
			if err != nil {
				slog.Warn("Error opening URL", "err", err)
			}
		}

		return nil
	},
}
