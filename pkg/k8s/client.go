package k8s

import (
	"path/filepath"

	"github.com/cockroachdb/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/kzh/sandbox/pkg/env"
)

type Client struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

func NewClient() (*Client, error) {
	var config *rest.Config

	switch env.Env() {
	case env.Development:
		home := homedir.HomeDir()
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
		if err != nil {
			return nil, errors.Wrap(err, "building kubeconfig")
		}
	case env.Production:
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, errors.Wrap(err, "getting in-cluster config")
		}
	default:
		return nil, errors.New("unsupported environment")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "creating kubernetes client")
	}

	_, err = clientset.ServerVersion()
	if err != nil {
		return nil, errors.Wrap(err, "getting server version")
	}

	return &Client{
		config:    config,
		clientset: clientset,
	}, nil
}
