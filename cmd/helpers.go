package cmd

import (
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/spf13/cobra"
)

// getDrupalContainer is a helper that extracts the context from args,
// gets the Docker client, and returns the Drupal container name.
// This is used by commands with DisableFlagParsing: true that need to manually handle --context.
func getDrupalContainer(cmd *cobra.Command, args []string) (filteredArgs []string, ctx *config.Context, cli *docker.DockerClient, containerName string, err error) {
	// Extract the --context flag from args since flag parsing is disabled
	filteredArgs, contextName, err := helpers.GetContextFromArgs(cmd, args)
	if err != nil {
		return nil, nil, nil, "", err
	}

	// Set the SDK's context so it uses the right configuration
	sdk.Config.Context = contextName

	// Use SDK helper to get the context configuration
	ctx, err = sdk.GetContext()
	if err != nil {
		return nil, nil, nil, "", err
	}

	// Use SDK helper to get the Docker client
	cli, err = sdk.GetDockerClient()
	if err != nil {
		return nil, nil, nil, "", err
	}

	// Get the Drupal container name
	containerName, err = cli.GetContainerName(ctx, *drupalServiceName)
	if err != nil {
		cli.Close()
		return nil, nil, nil, "", err
	}

	return filteredArgs, ctx, cli, containerName, nil
}

// getDrupalContainerFromFlags is a helper that uses normal flag parsing to get the context,
// Docker client, and Drupal container name.
// This is used by commands with normal flag parsing (not DisableFlagParsing).
func getDrupalContainerFromFlags(cmd *cobra.Command) (ctx *config.Context, cli *docker.DockerClient, containerName string, err error) {
	// Get context from flags using standard CurrentContext helper
	contextName, err := cmd.Flags().GetString("context")
	if err != nil {
		return nil, nil, "", err
	}

	// Set the SDK's context
	sdk.Config.Context = contextName

	// Use SDK helper to get the context configuration
	ctx, err = sdk.GetContext()
	if err != nil {
		return nil, nil, "", err
	}

	// Use SDK helper to get the Docker client
	cli, err = sdk.GetDockerClient()
	if err != nil {
		return nil, nil, "", err
	}

	// Get the Drupal container name
	containerName, err = cli.GetContainerName(ctx, *drupalServiceName)
	if err != nil {
		cli.Close()
		return nil, nil, "", err
	}

	return ctx, cli, containerName, nil
}
