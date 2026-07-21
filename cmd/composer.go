package cmd

import (
	"fmt"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/spf13/cobra"
)

var composerCmd = &cobra.Command{
	Use:                "composer [COMMAND...]",
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	Short:              "Run Composer commands inside the Drupal container",
	Long: `Run Composer commands inside the Drupal container of the active context.

Arguments are passed directly to Composer without shell interpretation. The command runs
from the context's configured Drupal container root.

Examples:
  sitectl drupal composer install
  sitectl drupal composer update -W
  sitectl drupal composer require 'drupal/islandora:^2.11'
  sitectl drupal composer --context prod install --no-dev`,
	RunE: func(cmd *cobra.Command, args []string) error {
		filteredArgs, ctx, cli, containerName, err := getDrupalContainer(cmd, args)
		if err != nil {
			return err
		}
		defer cli.Close()

		execOptions, err := composerExecOptions(ctx, containerName, filteredArgs)
		if err != nil {
			return err
		}
		exitCode, err := cli.Exec(cmd.Context(), execOptions)
		if err != nil {
			return err
		}

		if exitCode != 0 {
			return fmt.Errorf("non-zero exit code from command: %d", exitCode)
		}

		return nil
	},
}

func composerExecOptions(ctx *config.Context, containerName string, args []string) (docker.ExecOptions, error) {
	if strings.TrimSpace(containerName) == "" {
		return docker.ExecOptions{}, fmt.Errorf("no running Drupal container found")
	}

	composerArgs := make([]string, 1, len(args)+1)
	composerArgs[0] = "composer"
	composerArgs = append(composerArgs, args...)

	return docker.ExecOptions{
		Container:    containerName,
		Cmd:          composerArgs,
		WorkingDir:   ctx.EffectiveDrupalContainerRoot(),
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}, nil
}
