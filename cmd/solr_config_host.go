package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/pkg/sftp"
)

type localSolrConfigHostStore struct {
	projectRoot string
}

type remoteSolrConfigHostStore struct {
	client      *sftp.Client
	projectRoot string
}

func newSolrConfigHostStore(ctx *config.Context) (solrConfigHostStore, func() error, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("context is nil")
	}
	if ctx.DockerHostType != config.ContextRemote {
		return localSolrConfigHostStore{projectRoot: ctx.ProjectDir}, func() error { return nil }, nil
	}
	if sdk == nil {
		return nil, nil, fmt.Errorf("plugin SDK is unavailable")
	}
	sshClient, err := sdk.GetSSHClient()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to context host: %w", err)
	}
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, nil, fmt.Errorf("create context file client: %w", err)
	}
	return &remoteSolrConfigHostStore{client: sftpClient, projectRoot: ctx.ProjectDir}, sftpClient.Close, nil
}

func (s localSolrConfigHostStore) ReadTree(ctx context.Context, root string) (solrConfigTree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensureLocalSolrHostContainment(s.projectRoot, root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("tracked Solr config root %q is a symbolic link", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("tracked Solr config root %q is not a directory", root)
	}

	tree := solrConfigTree{}
	var total int64
	err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filename == root {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("tracked Solr config entry %q is a symbolic link", filename)
		}
		if entryInfo.IsDir() {
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("tracked Solr config entry %q is not a regular file", filename)
		}
		if len(tree) >= maxSolrConfigFiles {
			return fmt.Errorf("tracked Solr config contains more than %d files", maxSolrConfigFiles)
		}
		if entryInfo.Size() < 0 || entryInfo.Size() > maxSolrConfigFileBytes || total > maxSolrConfigExtractedBytes-entryInfo.Size() {
			return fmt.Errorf("tracked Solr config file %q exceeds size limits", filename)
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, err := cleanArchivePath(relative); err != nil {
			return err
		}
		file, err := os.Open(filename) // #nosec G304 -- filename is obtained from a walk rooted at the selected context project path.
		if err != nil {
			return err
		}
		data, readErr := readLimitedExact(file, entryInfo.Size(), maxSolrConfigFileBytes)
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		tree[relative] = data
		total += int64(len(data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return nil, err
	}
	return tree, nil
}

func (s localSolrConfigHostStore) PublishTree(ctx context.Context, root string, tree solrConfigTree) (err error) {
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureLocalSolrHostContainment(s.projectRoot, root); err != nil {
		return err
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	if err := ensureLocalSolrHostContainment(s.projectRoot, root); err != nil {
		return err
	}
	exists := false
	info, statErr := os.Lstat(root)
	switch {
	case statErr == nil:
		exists = true
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("tracked Solr config target %q is not a regular directory", root)
		}
	case !errors.Is(statErr, fs.ErrNotExist):
		return statErr
	}

	stage, err := os.MkdirTemp(parent, ".sitectl-solr-conf-stage-*")
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(stage); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove staged tracked Solr config: %w", cleanupErr))
		}
	}()
	if err := writeLocalSolrConfigTree(ctx, stage, tree); err != nil {
		return err
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}

	if !exists {
		if err := os.Rename(stage, root); err != nil {
			return err
		}
		return syncDirectory(parent)
	}

	suffix, err := secureSolrConfigSuffix()
	if err != nil {
		return err
	}
	backup := root + ".sitectl-backup-" + suffix
	if err := os.Rename(root, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, root); err != nil {
		restoreErr := os.Rename(backup, root)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous tracked Solr config: %w", restoreErr))
		}
		return err
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func writeLocalSolrConfigTree(ctx context.Context, root string, tree solrConfigTree) error {
	files := sortedSolrConfigFiles(tree)
	for _, relative := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination, err := safeLocalTreePath(root, relative)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) // #nosec G304 -- destination is constrained beneath a unique staging directory.
		if err != nil {
			return err
		}
		_, writeErr := file.Write(tree[relative])
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeLocalTreePath(root, relative string) (string, error) {
	cleaned, err := cleanArchivePath(relative)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(root, filepath.FromSlash(cleaned))
	contained, err := filepath.Rel(root, destination)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("solr config path %q escapes staging root", relative)
	}
	return destination, nil
}

func syncDirectory(directory string) (returnErr error) {
	handle, err := os.Open(directory) // #nosec G304 -- directory is a resolved staging or target parent path.
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := handle.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, closeErr)
		}
	}()
	return handle.Sync()
}

func (s *remoteSolrConfigHostStore) ReadTree(ctx context.Context, root string) (solrConfigTree, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("remote context file client is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root = path.Clean(root)
	if err := ensureRemoteSolrHostContainment(s.client, s.projectRoot, root); err != nil {
		return nil, err
	}
	rootInfo, err := s.client.Lstat(root)
	if err != nil {
		if remotePathMissing(err) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("tracked Solr config root %q is a symbolic link", root)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("tracked Solr config root %q is not a directory", root)
	}

	tree := solrConfigTree{}
	var total int64
	walker := s.client.Walk(root)
	for walker.Step() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := walker.Err(); err != nil {
			return nil, err
		}
		filename := path.Clean(walker.Path())
		if filename == root {
			continue
		}
		info := walker.Stat()
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("tracked Solr config entry %q is a symbolic link", filename)
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("tracked Solr config entry %q is not a regular file", filename)
		}
		if len(tree) >= maxSolrConfigFiles {
			return nil, fmt.Errorf("tracked Solr config contains more than %d files", maxSolrConfigFiles)
		}
		if info.Size() < 0 || info.Size() > maxSolrConfigFileBytes || total > maxSolrConfigExtractedBytes-info.Size() {
			return nil, fmt.Errorf("tracked Solr config file %q exceeds size limits", filename)
		}
		relative, err := remoteTreeRelative(root, filename)
		if err != nil {
			return nil, err
		}
		file, err := s.client.Open(filename)
		if err != nil {
			return nil, err
		}
		data, readErr := readLimitedExact(file, info.Size(), maxSolrConfigFileBytes)
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		tree[relative] = data
		total += int64(len(data))
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return nil, err
	}
	return tree, nil
}

func (s *remoteSolrConfigHostStore) PublishTree(ctx context.Context, root string, tree solrConfigTree) (err error) {
	if s == nil || s.client == nil {
		return fmt.Errorf("remote context file client is unavailable")
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root = path.Clean(root)
	if err := ensureRemoteSolrHostContainment(s.client, s.projectRoot, root); err != nil {
		return err
	}
	parent := path.Dir(root)
	if err := s.client.MkdirAll(parent); err != nil {
		return err
	}
	if err := ensureRemoteSolrHostContainment(s.client, s.projectRoot, root); err != nil {
		return err
	}
	exists := false
	info, statErr := s.client.Lstat(root)
	switch {
	case statErr == nil:
		exists = true
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("tracked Solr config target %q is not a regular directory", root)
		}
	case !remotePathMissing(statErr):
		return statErr
	}

	suffix, err := secureSolrConfigSuffix()
	if err != nil {
		return err
	}
	stage := path.Join(parent, ".sitectl-solr-conf-stage-"+suffix)
	if err := s.client.Mkdir(stage); err != nil {
		return err
	}
	defer func() {
		if cleanupErr := removeRemoteTree(s.client, stage); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove staged tracked Solr config: %w", cleanupErr))
		}
	}()
	if err := writeRemoteSolrConfigTree(ctx, s.client, stage, tree); err != nil {
		return err
	}

	if !exists {
		return s.client.PosixRename(stage, root)
	}
	backup := path.Join(parent, ".sitectl-solr-conf-backup-"+suffix)
	if err := s.client.PosixRename(root, backup); err != nil {
		return fmt.Errorf("atomically stage previous tracked Solr config: %w", err)
	}
	if err := s.client.PosixRename(stage, root); err != nil {
		restoreErr := s.client.PosixRename(backup, root)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous tracked Solr config: %w", restoreErr))
		}
		return err
	}
	if err := removeRemoteTree(s.client, backup); err != nil {
		return fmt.Errorf("remove previous tracked Solr config: %w", err)
	}
	return nil
}

func writeRemoteSolrConfigTree(ctx context.Context, client *sftp.Client, root string, tree solrConfigTree) error {
	for _, relative := range sortedSolrConfigFiles(tree) {
		if err := ctx.Err(); err != nil {
			return err
		}
		cleaned, err := cleanArchivePath(relative)
		if err != nil {
			return err
		}
		destination := path.Join(root, cleaned)
		if err := client.MkdirAll(path.Dir(destination)); err != nil {
			return err
		}
		file, err := client.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
		if err != nil {
			return err
		}
		if err := file.Chmod(0o644); err != nil {
			return errors.Join(err, file.Close())
		}
		_, writeErr := file.Write(tree[relative])
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func removeRemoteTree(client *sftp.Client, root string) error {
	walker := client.Walk(root)
	entries := []string{}
	for walker.Step() {
		if err := walker.Err(); err != nil {
			if remotePathMissing(err) {
				return nil
			}
			return err
		}
		entries = append(entries, walker.Path())
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		info, err := client.Lstat(entry)
		if err != nil {
			if remotePathMissing(err) {
				continue
			}
			return err
		}
		if info.IsDir() {
			if err := client.RemoveDirectory(entry); err != nil && !remotePathMissing(err) {
				return err
			}
			continue
		}
		if err := client.Remove(entry); err != nil && !remotePathMissing(err) {
			return err
		}
	}
	return nil
}

func remoteTreeRelative(root, filename string) (string, error) {
	root = path.Clean(root)
	filename = path.Clean(filename)
	prefix := root + "/"
	if !strings.HasPrefix(filename, prefix) {
		return "", fmt.Errorf("remote path %q is outside tree %q", filename, root)
	}
	relative := strings.TrimPrefix(filename, prefix)
	if _, err := cleanArchivePath(relative); err != nil {
		return "", err
	}
	return relative, nil
}

func remotePathMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such file") ||
		strings.Contains(message, "file does not exist") ||
		strings.Contains(message, "not found")
}

func sortedSolrConfigFiles(tree solrConfigTree) []string {
	files := make([]string, 0, len(tree))
	for filename := range tree {
		files = append(files, filename)
	}
	sort.Strings(files)
	return files
}

func ensureLocalSolrHostContainment(projectRoot, target string) error {
	if strings.TrimSpace(projectRoot) == "" {
		return fmt.Errorf("context project root is required for tracked Solr config access")
	}
	projectAbsolute, err := filepath.Abs(projectRoot)
	if err != nil {
		return err
	}
	projectReal, err := filepath.EvalSymlinks(projectAbsolute)
	if err != nil {
		return fmt.Errorf("resolve context project root %q: %w", projectRoot, err)
	}
	targetAbsolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	targetReal, err := resolveLocalPathWithMissing(targetAbsolute)
	if err != nil {
		return fmt.Errorf("resolve tracked Solr config target %q: %w", target, err)
	}
	relative, err := filepath.Rel(projectReal, targetReal)
	if err != nil {
		return err
	}
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("tracked Solr config target %q resolves outside context project %q", target, projectRoot)
	}
	return nil
}

func resolveLocalPathWithMissing(target string) (string, error) {
	current := filepath.Clean(target)
	missing := []string{}
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func ensureRemoteSolrHostContainment(client *sftp.Client, projectRoot, target string) error {
	if client == nil {
		return fmt.Errorf("remote context file client is unavailable")
	}
	if strings.TrimSpace(projectRoot) == "" {
		return fmt.Errorf("remote context project root is required for tracked Solr config access")
	}
	projectReal, err := client.RealPath(path.Clean(projectRoot))
	if err != nil {
		return fmt.Errorf("resolve remote context project root %q: %w", projectRoot, err)
	}
	targetReal, err := resolveRemotePathWithMissing(client, path.Clean(target))
	if err != nil {
		return fmt.Errorf("resolve remote tracked Solr config target %q: %w", target, err)
	}
	if targetReal == projectReal || !posixPathContained(projectReal, targetReal) {
		return fmt.Errorf("remote tracked Solr config target %q resolves outside context project %q", target, projectRoot)
	}
	return nil
}

func resolveRemotePathWithMissing(client *sftp.Client, target string) (string, error) {
	current := path.Clean(target)
	missing := []string{}
	for {
		_, err := client.Lstat(current)
		if err == nil {
			resolved, err := client.RealPath(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = path.Join(resolved, missing[index])
			}
			return path.Clean(resolved), nil
		}
		if !remotePathMissing(err) {
			return "", err
		}
		parent := path.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, path.Base(current))
		current = parent
	}
}
