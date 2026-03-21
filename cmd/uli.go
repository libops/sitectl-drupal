package cmd

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/spf13/cobra"
)

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

		exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
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
