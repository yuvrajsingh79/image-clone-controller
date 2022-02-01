//Package controller ...
package controller

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	err "errors"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeu "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/apps/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controller struct {
	depLister        listers.DeploymentLister
	daemonLister     listers.DaemonSetLister
	deploymentSynced cache.InformerSynced
	daemonsetSynced  cache.InformerSynced
	kubeClientSet    *kubernetes.Clientset
	workqueue        workqueue.RateLimitingInterface
	logger           *zap.Logger
}

// RunController ...
func RunController(k8sClientset *kubernetes.Clientset, ctxLogger *zap.Logger) {
	ctxLogger.Info("Starting the controller for updating daemonset and deployment pods")
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stopCh)
		<-sigCh
		os.Exit(1) // second signal. Exit directly.
	}()

	informerFactory := informers.NewSharedInformerFactory(k8sClientset, time.Second*30)

	depInformer := informerFactory.Apps().V1().Deployments()
	daemonInformer := informerFactory.Apps().V1().DaemonSets()

	c := &controller{
		depLister:        depInformer.Lister(),
		daemonLister:     daemonInformer.Lister(),
		deploymentSynced: depInformer.Informer().HasSynced,
		daemonsetSynced:  daemonInformer.Informer().HasSynced,
		workqueue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		kubeClientSet:    k8sClientset,
		logger:           ctxLogger,
	}

	depInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.syncDeploymentImage,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.syncDeploymentImage(newObj)
		},
		DeleteFunc: nil,
	})

	daemonInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.syncDaemonsetImage,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.syncDaemonsetImage(newObj)
		},
		DeleteFunc: nil,
	})

	informerFactory.Start(stopCh)

	if err := c.run(stopCh); err != nil {
		ctxLogger.Fatal("Failed to run the image controller ", zap.Error(err))
	}
}

// SyncDeploymentImage is triggered when a deployment is added to the cluster. It adds the new deployment to the workqueue.
func (c *controller) syncDeploymentImage(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtimeu.HandleError(err)
	}
	c.workqueue.Add("deployment/" + key)
}

// SyncDaemonsetImage is triggered when a daemonset is added to the cluster. It adds the new daemonset to the workqueue.
func (c *controller) syncDaemonsetImage(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtimeu.HandleError(err)
	}
	c.workqueue.Add("daemonset/" + key)
}

func (c *controller) run(stopCh <-chan struct{}) error {
	defer runtimeu.HandleCrash()
	defer c.workqueue.ShutDown()

	ok := cache.WaitForCacheSync(stopCh, c.deploymentSynced)
	if !ok {
		return err.New("failed to wait for deployment caches to sync")
	}
	ok = cache.WaitForCacheSync(stopCh, c.daemonsetSynced)
	if !ok {
		return err.New("failed to wait for daemonset caches to sync")
	}
	go wait.Until(c.runWorker, time.Second, stopCh)
	<-stopCh
	return nil
}

// runWorker takes one by one key from workqueue, checks if the namespace is not kube-system then calls the checkAndUpdateImage for updating image.
// If image is successfully updated It removes the obj from workqueue else it again requeues that obj to workqueue.
func (c *controller) runWorker() {
	processNext := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			runtimeu.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			runtimeu.HandleError(fmt.Errorf("invalid resource key: %s", key))
			return nil
		}
		if parts[1] != "kube-system" {
			c.logger.Info("Processing resource.", zap.Reflect("resourceType", parts[0]), zap.Reflect("Name", parts[2]))
			// If there is any error while updating image then again add the resource to workqueue
			if err := c.checkAndUpdateImage(context.TODO(), key, parts[0], parts[1], parts[2]); err != nil {
				c.workqueue.AddRateLimited(key)
				return fmt.Errorf("error in updating image for controller '%s'. Error: %s, Adding it again to workqueue", parts[1], err.Error())
			}
		}
		c.workqueue.Forget(obj)
		return nil
	}
	for {
		res, shutdown := c.workqueue.Get()
		if shutdown {
			return
		}
		if err := processNext(res); err != nil {
			runtimeu.HandleError(err)
		}
	}
}

// checkAndUpdateImage gets the container images of the newly deployment or daemonset and processes the image to push to backup registry and update the image.
// Returns err as nil if images are not updated. else returns nil
func (c *controller) checkAndUpdateImage(ctx context.Context, key, resourceType, namespace, name string) (err error) {
	var errs error
	var containers []corev1.Container
	var dep *appsv1.Deployment
	var daemonset *appsv1.DaemonSet
	c.logger.Info("Updating image for resource, ", zap.Reflect("key", key))

	if resourceType == "deployment" {
		dep, err = c.depLister.Deployments(namespace).Get(name)
	} else if resourceType == "daemonset" {
		daemonset, err = c.daemonLister.DaemonSets(namespace).Get(name)
	}

	if errs != nil {
		if errors.IsNotFound(err) {
			runtimeu.HandleError(fmt.Errorf("'%s' '%s' in work queue no longer exists", resourceType, name))
			return nil
		}
		return fmt.Errorf("error getting '%s'. error: %s", resourceType, err)
	}

	ready := false
	if resourceType == "deployment" {
		if isDeploymentReady(dep) {
			ready = true
			containers = dep.Spec.Template.Spec.Containers
		} else {
			return fmt.Errorf("deployment '%s' is not ready", name)
		}
	} else if resourceType == "daemonset" {
		if isDaemonSetReady(daemonset) {
			ready = true
			containers = daemonset.Spec.Template.Spec.Containers
		} else {
			return fmt.Errorf("daemonset '%s' is not ready", name)
		}
	}

	if ready {
		for i, cont := range containers {
			if imageNotPresent(cont.Image) {
				c.logger.Info("Processsing image", zap.Reflect("key", key), zap.Reflect("containerName", cont.Name))
				img, err := processImage(cont.Image)
				if err != nil {
					return fmt.Errorf("error in processing image for '%s'. ContainerName '%s'. Error: '%s'", key, cont.Name, err)
				}
				// update image
				c.logger.Info("Updating image in container spec for key, ", zap.Reflect("key", key), zap.Reflect("containerName", cont.Name))
				if resourceType == "deployment" {
					dep.Spec.Template.Spec.Containers[i].Image = img
					_, err = c.kubeClientSet.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
				} else if resourceType == "daemonset" {
					daemonset.Spec.Template.Spec.Containers[i].Image = img
					_, err = c.kubeClientSet.AppsV1().DaemonSets(namespace).Update(ctx, daemonset, metav1.UpdateOptions{})
				}
				if err == nil && !errors.IsConflict(err) {
					c.logger.Info("Updated image, ", zap.Reflect("NewImage", img))
					return nil
				}
				return err
			} else {
				c.logger.Info("Image is already present in registry for", zap.Reflect("key", key), zap.Reflect("containerName", cont.Name), zap.Reflect("imageName", cont.Image))
				return nil
			}
		}
	}
	return fmt.Errorf("'%s' '%s' in namespace '%s'is not in ready state", resourceType, name, namespace)
}

func isDeploymentReady(deployment *appsv1.Deployment) bool {
	status := deployment.Status
	desired := status.Replicas
	ready := status.ReadyReplicas
	if desired == ready && desired > 0 {
		return true
	}
	return false
}

func isDaemonSetReady(daemonsets *appsv1.DaemonSet) bool {
	status := daemonsets.Status
	desired := status.DesiredNumberScheduled
	ready := status.NumberReady
	if desired == ready && desired > 0 {
		return true
	}
	return false
}

func imageNotPresent(image string) bool {
	if len(repository) == 0 {
		return false
	} else if !strings.HasPrefix(image, repository) {
		return true
	}
	return false
}
