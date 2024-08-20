package executor

import (
	"context"

	"github.com/cockroachdb/errors"
	"k8s.io/client-go/rest"

	"github.com/kzh/sandbox/pkg/k8s"
)

type Service struct {
	namespace string
	config    *rest.Config
	client    *k8s.Client

	executors map[string]Executor
	workers   *k8s.Workers
}

type Executor interface {
	Init(ctx context.Context, service *Service) error
	Execute(ctx context.Context, code string) (string, error)
}

func NewService(ctx context.Context, namespace string) (*Service, error) {
	client, err := k8s.NewClient()
	if err != nil {
		return nil, errors.Wrap(err, "creating kubernetes client")
	}

	svc := &Service{
		namespace: namespace,
		client:    client,
		executors: make(map[string]Executor),
	}

	svc.workers = svc.client.NewWorkers(
		namespace,
		"worker",
		"debian:bookworm",
		5,
		true,
	)
	svc.workers.LimitResource(2, 200)
	if err := svc.workers.Start(ctx); err != nil {
		return nil, errors.Wrap(err, "starting workers")
	}

	executors := map[string]Executor{
		"golang": NewGolangExecutor(svc),
	}
	for name, executor := range executors {
		if err := svc.RegisterExecutor(context.Background(), name, executor); err != nil {
			return nil, errors.Wrapf(err, "registering executor %s", name)
		}
	}

	return svc, nil
}

func (svc *Service) RegisterExecutor(ctx context.Context, name string, executor Executor) error {
	if err := executor.Init(ctx, svc); err != nil {
		return errors.Wrap(err, "initializing executor")
	}
	svc.executors[name] = executor
	return nil
}

func (svc *Service) Execute(ctx context.Context, code string) (string, error) {
	out, err := svc.executors["golang"].Execute(ctx, code)
	if err != nil {
		return "", errors.Wrap(err, "executing code")
	}
	return out, nil
}
