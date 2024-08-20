package k8s

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
)

var (
	GvisorRuntimeClass      = "gvisor"
	DeletePropagationPolicy = metav1.DeletePropagationBackground
	TerminationGracePeriod  = int64(1)
	ServiceLinks            = false
	AutoMountServiceAccount = false
)

type Worker struct {
	pod *corev1.Pod
}

func (w *Worker) Name() string {
	return w.pod.Name
}

type Workers struct {
	deployment *appsv1.Deployment
	pods       sync.Map
	workers    chan *Worker
	stop       chan struct{}

	name      string
	namespace string
	image     string
	size      int32
	sandboxed bool

	cpu int
	mem int

	k8s *Client
}

func (c *Client) NewWorkers(namespace string, name string, image string, size int32, sandboxed bool) *Workers {
	return &Workers{
		name:      name,
		namespace: namespace,
		image:     image,
		size:      size,
		sandboxed: sandboxed,

		k8s: c,

		pods:    sync.Map{},
		workers: make(chan *Worker, size),
	}
}

func (w *Workers) LimitResource(cpu int, mem int) {
	w.cpu = cpu
	w.mem = mem
}

func (w *Workers) Start(ctx context.Context) error {
	if err := w.sync(); err != nil {
		return errors.Wrap(err, "sync")
	}

	deployment, err := w.deploy(ctx)
	if err != nil {
		return errors.Wrap(err, "deploy")
	}
	w.deployment = deployment
	return nil
}

func (w *Workers) sync() error {
	selector := labels.SelectorFromSet(labels.Set{"sandbox/pool": w.name})
	factory := informers.NewSharedInformerFactoryWithOptions(w.k8s.clientset, time.Second, informers.WithNamespace(w.namespace), informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.LabelSelector = selector.String()
	}))
	informer := factory.Core().V1().Pods().Informer()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			if IsPodReady(pod) {
				w.register(pod)
				w.pods.Store(pod.Name, struct{}{})
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldPod, pod := oldObj.(*corev1.Pod), newObj.(*corev1.Pod)
			if IsPodReady(oldPod) || !IsPodReady(pod) {
				return
			}

			w.register(pod)
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			w.pods.Delete(pod.Name)
		},
	})
	if err != nil {
		return err
	}

	w.stop = make(chan struct{})
	go factory.Start(w.stop)
	return nil
}

func (w *Workers) register(pod *corev1.Pod) {
	w.workers <- &Worker{pod: pod}
}

func (w *Workers) deploy(ctx context.Context) (*appsv1.Deployment, error) {
	deployments := w.k8s.clientset.AppsV1().Deployments(w.namespace)
	deployment, err := deployments.Get(ctx, w.name, metav1.GetOptions{})
	if err == nil {
		return deployment, nil
	}

	podLabels := map[string]string{
		"sandbox/pool":    w.name,
		"sandbox/claimed": "false",
	}

	resources := corev1.ResourceList{}
	if w.cpu != 0 {
		resources[corev1.ResourceCPU] = resource.MustParse(fmt.Sprintf("%dm", w.cpu*100))
	}
	if w.mem != 0 {
		resources[corev1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%dMi", w.mem))
	}

	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    w.name,
				Image:   w.image,
				Command: []string{"sleep", "infinity"},
				Resources: corev1.ResourceRequirements{
					Requests: resources,
				},
			},
		},
		AutomountServiceAccountToken:  &AutoMountServiceAccount,
		EnableServiceLinks:            &ServiceLinks,
		TerminationGracePeriodSeconds: &TerminationGracePeriod,
	}

	if w.sandboxed {
		spec.RuntimeClassName = &GvisorRuntimeClass
	}

	deployment, err = deployments.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: w.name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &w.size,
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: spec,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "creating deployment")
	}

	return deployment, nil
}

func IsPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			return false
		}
	}

	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	return true
}

func (w *Workers) Acquire(ctx context.Context) (*Worker, error) {
	var worker *Worker
	select {
	case worker = <-w.workers:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	worker.pod.Labels["sandbox/claimed"] = "true"
	worker.pod.SetOwnerReferences(nil)

	pods := w.k8s.clientset.CoreV1().Pods(w.namespace)
	var err error
	if worker.pod, err = pods.Update(ctx, worker.pod, metav1.UpdateOptions{}); err != nil {
		return nil, errors.Wrap(err, "updating pod")
	}

	return worker, nil
}

func (w *Workers) Exec(ctx context.Context, worker *Worker, command []string, stdin []byte) ([]byte, error) {
	req := w.k8s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(worker.pod.Name).
		Namespace(w.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: command,
			Stdin:   len(stdin) != 0,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(w.k8s.config, "POST", req.URL())
	if err != nil {
		return nil, errors.Wrap(err, "creating executor")
	}

	var stdout, stderr bytes.Buffer
	options := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if len(stdin) != 0 {
		options.Stdin = bytes.NewReader(stdin)
	}

	err = executor.StreamWithContext(ctx, options)
	if err != nil {
		if stderr.Len() != 0 {
			return nil, errors.New(stderr.String())
		}

		return nil, errors.Wrap(err, "streaming")
	}

	return stdout.Bytes(), nil
}

func (w *Workers) Write(ctx context.Context, worker *Worker, file string, content []byte) error {
	_, err := w.Exec(ctx, worker, []string{"/bin/sh", "-c", fmt.Sprintf("cat > %s", file)}, content)
	if err != nil {
		return errors.Wrap(err, "writing file")
	}
	return nil
}

func (w *Workers) Read(ctx context.Context, worker *Worker, file string) ([]byte, error) {
	stdout, err := w.Exec(ctx, worker, []string{"/bin/sh", "-c", fmt.Sprintf("cat %s", file)}, nil)
	if err != nil {
		return nil, errors.Wrap(err, "reading file")
	}
	return stdout, nil
}

func (w *Workers) Release(ctx context.Context, worker *Worker) error {
	if err := w.k8s.clientset.CoreV1().Pods(w.namespace).Delete(ctx, worker.pod.Name, metav1.DeleteOptions{
		PropagationPolicy: &DeletePropagationPolicy,
	}); err != nil {
		return errors.Wrap(err, "deleting pod")
	}
	return nil
}
