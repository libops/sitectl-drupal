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

const (
	maxConfigArchiveBytes           = 64 << 20
	maxCompressedConfigArchiveBytes = 64 << 20
	drushExecutable                 = "/var/www/drupal/vendor/bin/drush"
	crosswalkConfigSnapshotScript   = `set -eu
set --
for file in \
  config/sync/system.site.yml \
  config/sync/field.storage.*.yml \
  config/sync/field.field.*.yml \
  config/sync/rdf.mapping.*.yml
do
  if [ -f "$file" ]; then
    set -- "$@" "$file"
  fi
done
[ "$#" -gt 0 ]
tar -czf - "$@"`
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
		Cmd:          []string{drushExecutable, "sql-dump", "-y", "--skip-tables-list=cache,cache_*,watchdog", "--structure-tables-list=cache,cache_*,watchdog", "--debug"},
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
	ok, err := corejob.ConfirmDatabaseReplacement(ctx.Name, "Drupal", inputPath, yolo)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("database import cancelled")
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
	if err := tempFile.Close(); err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err := corejob.DownloadContextFile(ctx, inputPath, tempPath); err != nil {
		return err
	}
	inputFile, err := os.Open(tempPath) // #nosec G304 -- tempPath is created by this process and populated before import.
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
		Cmd:          []string{drushExecutable, "sql:cli"},
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
	_, err = docker.ExecCapture(cmd.Context(), cli, containerName, ctx.EffectiveDrupalContainerRoot(), []string{drushExecutable, "cr", "-y"})
	return err
}

func RunConfigExport(cmd *cobra.Command, ctx *config.Context, outputPath string) error {
	if err := corejob.EnsurePathAbsentOnContext(ctx, outputPath); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "sitectl-drupal-config-export-*.tar.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()

	if err := WriteConfigExport(cmd, ctx, tempFile); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return ctx.UploadFile(tempPath, outputPath)
}

// WriteConfigExport exports active Drupal configuration and writes a bounded
// gzip-compressed config/sync tar stream to output. The caller owns output.
func WriteConfigExport(cmd *cobra.Command, ctx *config.Context, output io.Writer) error {
	return writeActiveConfigArchive(cmd, ctx, output, []string{"tar", "-czf", "-", "config/sync"})
}

// WriteCrosswalkConfigSnapshot exports active Drupal configuration and writes
// only the model inputs Crosswalk understands. Limiting the artifact to site,
// field, and RDF mapping documents keeps unrelated operational configuration
// and credential-bearing environment values outside the profile workflow.
func WriteCrosswalkConfigSnapshot(cmd *cobra.Command, ctx *config.Context, output io.Writer) error {
	return writeActiveConfigArchive(cmd, ctx, output, []string{"sh", "-c", crosswalkConfigSnapshotScript})
}

func writeActiveConfigArchive(cmd *cobra.Command, ctx *config.Context, output io.Writer, archiveCommand []string) error {
	if output == nil {
		return fmt.Errorf("drupal config export output is required")
	}
	if len(archiveCommand) == 0 {
		return fmt.Errorf("drupal config archive command is required")
	}
	_, cli, containerName, err := getDrupalContainerForContext(cmd.Context(), ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerRoot := ctx.EffectiveDrupalContainerRoot()
	if _, err := docker.ExecCapture(cmd.Context(), cli, containerName, containerRoot, []string{drushExecutable, "cex", "-y"}); err != nil {
		return fmt.Errorf("export active Drupal configuration with Drush: %w", err)
	}

	var stderr bytes.Buffer
	archiveWriter := &maximumBytesWriter{
		writer:    output,
		remaining: maxCompressedConfigArchiveBytes,
		maximum:   maxCompressedConfigArchiveBytes,
	}
	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          archiveCommand,
		WorkingDir:   containerRoot,
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       archiveWriter,
		Stderr:       &stderr,
	})
	if err != nil {
		return fmt.Errorf("stream Drupal config export: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("drupal config export failed with exit code %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type maximumBytesWriter struct {
	writer    io.Writer
	remaining int64
	maximum   int64
}

func (w *maximumBytesWriter) Write(data []byte) (int, error) {
	if int64(len(data)) <= w.remaining {
		n, err := w.writer.Write(data)
		w.remaining -= int64(n)
		return n, err
	}

	allowed := int(w.remaining)
	written := 0
	if allowed > 0 {
		n, err := w.writer.Write(data[:allowed])
		written += n
		w.remaining -= int64(n)
		if err != nil {
			return written, err
		}
	}
	return written, fmt.Errorf("drupal config export exceeds %d compressed bytes", w.maximum)
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
	if err := tempFile.Close(); err != nil {
		return err
	}
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
	if _, err := docker.ExecCapture(cmd.Context(), cli, containerName, containerRoot, []string{drushExecutable, "cim", "-y"}); err != nil {
		return err
	}
	_, err = docker.ExecCapture(cmd.Context(), cli, containerName, containerRoot, []string{drushExecutable, "cr", "-y"})
	return err
}

func getDrupalContainerForContext(runCtx context.Context, ctx *config.Context) (*config.Context, *docker.DockerClient, string, error) {
	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return nil, nil, "", err
	}

	containerName, err := cli.GetContainerNameContext(runCtx, ctx, drupalService)
	if err != nil {
		_ = cli.Close()
		return nil, nil, "", err
	}
	if strings.TrimSpace(containerName) == "" {
		_ = cli.Close()
		return nil, nil, "", fmt.Errorf("unable to find drupal service %q for context %q", drupalService, ctx.Name)
	}

	return ctx, cli, containerName, nil
}

func resolveContextDrupalConfigDir(ctx *config.Context, drupalRootfs string) (string, error) {
	rootfs := strings.TrimSpace(drupalRootfs)
	if rootfs == "" {
		rootfs = ctx.EffectiveDrupalRootfs()
	}
	return ctx.ResolveProjectPath(filepath.Join(rootfs, "config", "sync")), nil
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
	destination = filepath.Clean(destination)
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()

	file, err := os.Open(archivePath) // #nosec G304 -- archivePath is a temporary file created by this process before extracting under destination.
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

		relPath, err := cleanTarRelPath(header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(relPath, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > maxConfigArchiveBytes {
				return fmt.Errorf("tar entry %q exceeds maximum allowed size", header.Name)
			}
			if err := root.MkdirAll(filepath.Dir(relPath), 0o700); err != nil {
				return err
			}
			out, err := root.OpenFile(relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(out, tarReader, header.Size); err != nil {
				_ = out.Close()
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

func cleanTarRelPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty tar entry name")
	}
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe tar entry path %q", name)
	}
	return cleaned, nil
}
