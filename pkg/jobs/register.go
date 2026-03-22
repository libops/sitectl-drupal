package jobs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	corejob "github.com/libops/sitectl/pkg/job"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	sdk           *plugin.SDK
	drupalService = "drupal"
)

func Register(s *plugin.SDK) {
	sdk = s
	sdk.RegisterContextJob(corejob.Spec{Name: "db-backup", Description: "Export a Drupal database backup artifact"}, &dbBackupJob{})
	sdk.RegisterContextJob(corejob.Spec{Name: "db-import", Description: "Import a Drupal database backup artifact"}, &dbImportJob{})
	sdk.RegisterContextJob(corejob.Spec{Name: "config-export", Description: "Export Drupal config to a tar.gz artifact"}, &configExportJob{})
	sdk.RegisterContextJob(corejob.Spec{Name: "config-import", Description: "Import Drupal config from a tar.gz artifact"}, &configImportJob{})
}

type dbBackupJob struct {
	Output string
}

func (j *dbBackupJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Output, "output", "", "Absolute output path on the host for the context this job runs on")
}

func (j *dbBackupJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Output) == "" {
		return fmt.Errorf("--output is required")
	}
	return RunDBBackup(cmd, ctx, j.Output)
}

type dbImportJob struct {
	Input string
	Yolo  bool
}

func (j *dbImportJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Input, "input", "", "Absolute input path on the host for the context this job runs on")
	cmd.Flags().BoolVar(&j.Yolo, "yolo", false, "Apply destructive database changes without confirmation")
}

func (j *dbImportJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Input) == "" {
		return fmt.Errorf("--input is required")
	}
	return RunDBImport(cmd, ctx, j.Input, j.Yolo)
}

type configExportJob struct {
	Output string
}

func (j *configExportJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Output, "output", "", "Absolute output path on the host for the context this job runs on")
}

func (j *configExportJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Output) == "" {
		return fmt.Errorf("--output is required")
	}
	return RunConfigExport(cmd, ctx, j.Output)
}

type configImportJob struct {
	Input        string
	DrupalRootfs string
}

func (j *configImportJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Input, "input", "", "Absolute input path on the host for the context this job runs on")
	cmd.Flags().StringVar(&j.DrupalRootfs, "drupal-rootfs", j.DrupalRootfs, "Drupal rootfs relative to the context project dir")
}

func (j *configImportJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Input) == "" {
		return fmt.Errorf("--input is required")
	}
	return RunConfigImport(cmd, ctx, j.Input, j.DrupalRootfs)
}

func RunDBBackup(cmd *cobra.Command, ctx *config.Context, outputPath string) error {
	if err := corejob.EnsurePathAbsentOnContext(ctx, outputPath); err != nil {
		return err
	}
	_, cli, containerName, err := getDrupalContainerForContext(cmd.Context(), ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	tempFile, err := os.CreateTemp("", "sitectl-drupal-db-backup-*.sql.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()

	gzipWriter := gzip.NewWriter(tempFile)
	defer gzipWriter.Close()

	var stderr bytes.Buffer
	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          []string{"drush", "sql-dump", "-y", "--skip-tables-list=cache,cache_*,watchdog", "--structure-tables-list=cache,cache_*,watchdog", "--debug"},
		WorkingDir:   ctx.EffectiveDrupalContainerRoot(),
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       gzipWriter,
		Stderr:       &stderr,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("drupal sql-dump failed with exit code %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return ctx.UploadFile(tempPath, outputPath)
}

func RunDBImport(cmd *cobra.Command, ctx *config.Context, inputPath string, yolo bool) error {
	if !yolo {
		ok, err := confirmDatabaseReplacement(ctx.Name, "Drupal", inputPath)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("database import cancelled")
		}
	}

	_, cli, containerName, err := getDrupalContainerForContext(cmd.Context(), ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	tempFile, err := os.CreateTemp("", "sitectl-drupal-db-import-*.sql.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if err := corejob.DownloadContextFile(ctx, inputPath, tempPath); err != nil {
		return err
	}
	inputFile, err := os.Open(tempPath)
	if err != nil {
		return err
	}
	defer inputFile.Close()

	gzipReader, err := gzip.NewReader(inputFile)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	var stderr bytes.Buffer
	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          []string{"drush", "sql:cli"},
		WorkingDir:   ctx.EffectiveDrupalContainerRoot(),
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Stdin:        gzipReader,
		Stdout:       io.Discard,
		Stderr:       &stderr,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("drupal sql import failed with exit code %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
	_, err = execDrupalCommandCapture(cmd.Context(), cli, containerName, ctx.EffectiveDrupalContainerRoot(), []string{"drush", "cr", "-y"})
	return err
}

func RunConfigExport(cmd *cobra.Command, ctx *config.Context, outputPath string) error {
	if err := corejob.EnsurePathAbsentOnContext(ctx, outputPath); err != nil {
		return err
	}
	_, cli, containerName, err := getDrupalContainerForContext(cmd.Context(), ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerRoot := ctx.EffectiveDrupalContainerRoot()
	if _, err := execDrupalCommandCapture(cmd.Context(), cli, containerName, containerRoot, []string{"drush", "cex", "-y"}); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "sitectl-drupal-config-export-*.tar.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()

	var stderr bytes.Buffer
	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          []string{"tar", "-czf", "-", "config/sync"},
		WorkingDir:   containerRoot,
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       tempFile,
		Stderr:       &stderr,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("drupal config export failed with exit code %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return ctx.UploadFile(tempPath, outputPath)
}

func RunConfigImport(cmd *cobra.Command, ctx *config.Context, inputPath, drupalRootfs string) error {
	configDir, err := resolveContextDrupalConfigDir(ctx, drupalRootfs)
	if err != nil {
		return err
	}
	if err := corejob.EnsureDirOnContext(ctx, configDir); err != nil {
		return err
	}
	_, cli, containerName, err := getDrupalContainerForContext(cmd.Context(), ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	tempFile, err := os.CreateTemp("", "sitectl-drupal-config-import-*.tar.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if err := corejob.DownloadContextFile(ctx, inputPath, tempPath); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "sitectl-drupal-config-import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	if err := extractTarGz(tempPath, tempDir); err != nil {
		return err
	}

	files, err := ctx.NewFileAccessor()
	if err != nil {
		return err
	}
	defer files.Close()

	if err := files.RemoveAll(configDir); err != nil {
		return err
	}
	if err := files.MkdirAll(configDir); err != nil {
		return err
	}
	if err := uploadDirectory(files, filepath.Join(tempDir, "config", "sync"), configDir); err != nil {
		return err
	}

	containerRoot := ctx.EffectiveDrupalContainerRoot()
	if _, err := execDrupalCommandCapture(cmd.Context(), cli, containerName, containerRoot, []string{"drush", "cim", "-y"}); err != nil {
		return err
	}
	_, err = execDrupalCommandCapture(cmd.Context(), cli, containerName, containerRoot, []string{"drush", "cr", "-y"})
	return err
}

func getDrupalContainerForContext(runCtx context.Context, ctx *config.Context) (*config.Context, *docker.DockerClient, string, error) {
	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return nil, nil, "", err
	}

	containerName, err := cli.GetContainerNameContext(runCtx, ctx, drupalService)
	if err != nil {
		cli.Close()
		return nil, nil, "", err
	}
	if strings.TrimSpace(containerName) == "" {
		cli.Close()
		return nil, nil, "", fmt.Errorf("unable to find drupal service %q for context %q", drupalService, ctx.Name)
	}

	return ctx, cli, containerName, nil
}

func execDrupalCommandCapture(runCtx context.Context, cli *docker.DockerClient, containerName, containerRoot string, command []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := cli.Exec(runCtx, docker.ExecOptions{
		Container:    containerName,
		Cmd:          command,
		WorkingDir:   containerRoot,
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       &stdout,
		Stderr:       &stderr,
	})
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return "", fmt.Errorf("drupal command failed with exit code %d: %s", exitCode, detail)
		}
		return "", fmt.Errorf("drupal command failed with exit code %d", exitCode)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func resolveContextDrupalConfigDir(ctx *config.Context, drupalRootfs string) (string, error) {
	rootfs := strings.TrimSpace(drupalRootfs)
	if rootfs == "" {
		rootfs = ctx.EffectiveDrupalRootfs()
	}
	return ctx.ResolveProjectPath(filepath.Join(rootfs, "config", "sync")), nil
}

func confirmDatabaseReplacement(targetContext, databaseName, inputPath string) (bool, error) {
	prompt := []string{
		fmt.Sprintf("About to import %s database artifact %q into context %q.", databaseName, inputPath, targetContext),
		"This will wipe out the target database.",
		"Continue? [y/N]: ",
	}

	input, err := config.GetInput(prompt...)
	if err != nil {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func uploadDirectory(files *config.FileAccessor, sourceDir, destinationDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		return files.UploadFile(path, filepath.Join(destinationDir, relPath))
	})
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(destination, filepath.Clean(header.Name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			out, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tarReader); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry type %q", string(header.Typeflag))
		}
	}
}
