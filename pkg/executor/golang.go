package executor

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap"

	"github.com/kzh/sandbox/pkg/k8s"
)

var _ Executor = (*GolangExecutor)(nil)

type GolangExecutor struct {
	builders *k8s.Workers

	service *Service
}

func NewGolangExecutor(service *Service) *GolangExecutor {
	return &GolangExecutor{
		service: service,
	}
}

func (g *GolangExecutor) Init(ctx context.Context, service *Service) error {
	g.builders = g.service.client.NewWorkers(
		service.namespace,
		"builder-golang",
		"ghcr.io/kzh/golang-builder:latest",
		5,
		false,
	)
	if err := g.builders.Start(ctx); err != nil {
		return errors.Wrap(err, "starting builders")
	}

	return nil
}

func (g *GolangExecutor) Execute(ctx context.Context, code string) (string, error) {
	builder, err := g.builders.Acquire(ctx)
	if err != nil {
		return "", errors.Wrap(err, "fetching builder")
	}
	defer g.builders.Release(ctx, builder)
	zap.S().Infow("fetching builder", "pod", builder.Name())

	if err := g.builders.Write(ctx, builder, "/app/main.go", []byte(code)); err != nil {
		return "", errors.Wrap(err, "writing code")
	}

	start := time.Now()
	if _, err := g.builders.Exec(
		ctx, builder, []string{"go", "build", "-o", "/app/main", "-ldflags", "-s -w", "/app/main.go"}, nil,
	); err != nil {
		return "", errors.Wrap(err, "building code")
	}
	zap.S().Infow("finished building", "duration", time.Since(start))

	prog, err := g.builders.Read(ctx, builder, "/app/main")
	if err != nil {
		return "", errors.Wrap(err, "reading executable")
	}

	workers := g.service.workers
	runner, err := workers.Acquire(ctx)
	if err != nil {
		return "", errors.Wrap(err, "fetching runner")
	}
	defer workers.Release(ctx, runner)
	zap.S().Infow("fetching runner", "pod", runner.Name())

	if err := workers.Write(ctx, runner, "main", prog); err != nil {
		return "", errors.Wrap(err, "writing executable")
	}

	if _, err := workers.Exec(
		ctx, runner, []string{"chmod", "+x", "main"}, nil,
	); err != nil {
		return "", errors.Wrap(err, "setting executable permissions")
	}

	start = time.Now()
	out, err := workers.Exec(
		ctx, runner, []string{"./main"}, nil,
	)
	if err != nil {
		return "", errors.Wrap(err, "running code")
	}
	zap.S().Infow("finished running", "duration", time.Since(start))

	return string(out), nil
}
