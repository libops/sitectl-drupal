package cmd

import (
	"context"
	"fmt"
	"path"

	"github.com/libops/sitectl/pkg/docker"
)

const solrConfigTargetLockFile = ".sitectl-solr-config.lock"

func (r *dockerSolrConfigRuntime) AcquireLock(ctx context.Context, container, corePath string) (context.Context, func() error, error) {
	if r == nil || r.client == nil {
		return nil, nil, fmt.Errorf("docker client is unavailable")
	}
	if _, err := r.ExecCapture(ctx, container, "", []string{"mkdir", "-p", corePath}); err != nil {
		return nil, nil, fmt.Errorf("create solr core directory for target lock: %w", err)
	}
	lock, err := r.client.AcquireContainerFileLock(ctx, docker.ContainerFileLockOptions{
		Container: container,
		Path:      path.Join(corePath, solrConfigTargetLockFile),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("acquire target solr config lock: %w", err)
	}
	return lock.Context(), lock.Release, nil
}
