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
	Short: "Generate a one-time login link and open it in your browser",
	Long: `Generate a one-time login link and automatically open it in your default browser.

This runs drush uli inside the Drupal container, captures the resulting URL, and opens it.
Unlike running drush uli directly, this command handles browser launching for you.

Examples:
  sitectl isle uli           # Login as admin (user 1)
  sitectl isle uli --uid=2   # Login as user ID 2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cli, containerName, err := getDrupalContainerFromFlags(cmd)
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
		drushCmd := []string{"drush", "uli", fmt.Sprintf("--uid=%d", uid)}

		exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
			Container:    containerName,
			Cmd:          drushCmd,
			WorkingDir:   ctx.EffectiveDrupalContainerRoot(),
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
		fmt.Fprintln(cmd.OutOrStdout(), output)

		if strings.HasPrefix(output, "http") {
			err := helpers.OpenURL(output)
			if err != nil {
				slog.Warn("Error opening URL", "err", err)
			}
		}

		return nil
	},
}
