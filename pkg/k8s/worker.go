package k8s

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
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

	pods map[string]*corev1.Pod
	sub  map[chan<- *Worker]struct{}
	mu   sync.Mutex

	stopper func()

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

		pods: make(map[string]*corev1.Pod, size*2),
		sub:  make(map[chan<- *Worker]struct{}),
	}
}

func (w *Workers) LimitResource(cpu int, mem int) {
	w.cpu = cpu
	w.mem = mem
}

func (w *Workers) Start(ctx context.Context) error {
	go w.sync()

	deployment, err := w.deploy(ctx)
	if err != nil {
		return errors.Wrap(err, "deploy")
	}
	w.deployment = deployment
	return nil
}

func (w *Workers) sync() {
	for {
		func() {
			ctx := context.Background()
			selector := labels.SelectorFromSet(labels.Set{
				"sandbox/pool":    w.name,
				"sandbox/claimed": "false",
			})
			pods, err := w.k8s.clientset.CoreV1().Pods(w.namespace).Watch(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
			if err != nil {
				zap.S().Warnw("pod watch", "error", err)
				time.Sleep(time.Second)
				return
			}
			defer pods.Stop()

			for event := range pods.ResultChan() {
				switch event.Type {
				case watch.Added, watch.Modified:
					pod := event.Object.(*corev1.Pod)
					w.ingest(pod)
				case watch.Error:
					zap.S().Warnw("pod watch", "object", event.Object)
				}
			}
		}()
		zap.S().Warnw("pod watch closed", "group", w.name)
	}
}

func (w *Workers) ingest(pod *corev1.Pod) {
	if IsPodReady(pod) {
		w.mu.Lock()
		_, ok := w.pods[pod.Name]
		if ok {
			w.mu.Unlock()
			return
		}
		zap.S().Infow("pod added", "group", w.name, "pod", pod.Name)

		var ch chan<- *Worker
		for ch = range w.sub {
			delete(w.sub, ch)
			break
		}

		if ch == nil {
			w.pods[pod.Name] = pod
		} else {
			select {
			case ch <- &Worker{pod}:
			default:
				zap.S().Error("unable to send worker")
			}
		}
		w.mu.Unlock()
	} else {
		w.mu.Lock()
		_, ok := w.pods[pod.Name]
		if !ok {
			w.mu.Unlock()
			return
		}
		zap.S().Infow("pod removed", "group", w.name, "pod", pod.Name)

		delete(w.pods, pod.Name)
		w.mu.Unlock()
	}
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

	if pod.DeletionTimestamp != nil {
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
	var ch chan *Worker
	w.mu.Lock()
	zap.S().Infow("acquiring worker", "group", w.name, "workers", slices.Collect(maps.Keys(w.pods)), "waiting", len(w.sub))

	for name, pod := range w.pods {
		worker = &Worker{pod}
		delete(w.pods, name)
		break
	}
	if worker == nil {
		ch = make(chan *Worker, 1)
		defer close(ch)
		w.sub[ch] = struct{}{}
	}
	w.mu.Unlock()

	if worker == nil {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			delete(w.sub, ch)
			w.mu.Unlock()
			return nil, errors.Wrap(ctx.Err(), "no worker available")
		case worker = <-ch:
		}
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
