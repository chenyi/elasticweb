/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	elasticwebv1 "github.com/chenyi/elasticweb/api/v1"
)

// ElasticWebReconciler reconciles a ElasticWeb object
type ElasticWebReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=elasticweb.com.alibaba-inc.chenyi,resources=elasticwebs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=elasticweb.com.alibaba-inc.chenyi,resources=elasticwebs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ElasticWeb object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ElasticWebReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.WithValues("ElasticWeb", req.NamespacedName)

	// your logic here
	logger.Info("1. start reconcile logic")

	// ?????????????????????
	instance := &elasticwebv1.ElasticWeb{}

	// ?????????????????????????????????
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		// ????????????????????????????????????????????????reconcile
		if errors.IsNotFound(err) {
			logger.Info("2.1 instance not found, maybe removed already")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "2.2 Error")
	}
	logger.Info("3. instance is " + instance.String())

	// ??????deployment
	deployment := &v1.Deployment{}
	err = r.Get(ctx, req.NamespacedName, deployment)

	// ????????????
	if err != nil {
		// ???????????????????????????
		if errors.IsNotFound(err) {
			// ?????????????????????????????????????????????
			logger.Info("4. deployment not found")

			// ????????????
			if *instance.Spec.TotalQPS < 1 {
				logger.Info("5.1 no need deployment, TotalQPS is less than 1 ")
				return ctrl.Result{}, nil
			}
			// proceed to create deployment and service
			err = r.createServiceIfNotExist(ctx, instance, req)
			if err != nil {
				logger.Error(err, "createServiceIfNotExist error")
				return ctrl.Result{}, err
			}

			err = r.createDeploymentIfNotExist(ctx, instance, req)
			if err != nil {
				logger.Error(err, "createDeploymentIfNotExist error")
				return ctrl.Result{}, err
			}

			err = r.updateStatus(ctx, instance)
			if err != nil {
				logger.Error(err, "updateStatus error")
				return ctrl.Result{}, err
			}

			// ?????????????????????????????????????????????????????????????????????
			return ctrl.Result{}, nil
		} else {
			// ???????????????????????????
			logger.Error(err, "7. error")
			return ctrl.Result{}, err
		}
	}

	expectReplicas := getExpectReplicas(instance)
	realReplicas := *(deployment.Spec.Replicas)

	logger.Info(fmt.Sprintf("9. expectReplicas [%d], realReplicas [%d]", expectReplicas, realReplicas))

	if expectReplicas == realReplicas {
		logger.Info("10. return now")
		return ctrl.Result{}, nil
	}

	// ???????????????????????????
	*(deployment.Spec.Replicas) = expectReplicas
	logger.Info("11. update deployment's Replicas")
	// ?????????????????????deployment
	if err = r.Update(ctx, deployment); err != nil {
		logger.Error(err, "12. update deployment replicas error")
		// ???????????????????????????
		return ctrl.Result{}, err
	}

	logger.Info("13. update status")

	// ????????????deployment???Replicas????????????????????????
	if err = r.updateStatus(ctx, instance); err != nil {
		logger.Error(err, "14. update status error")
		// ???????????????????????????
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ElasticWebReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticwebv1.ElasticWeb{}).
		Complete(r)
}

func (r *ElasticWebReconciler) createServiceIfNotExist(ctx context.Context, instance *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	logger := log.FromContext(ctx)
	logger.WithValues("func", "createService")

	// ?????????service??????
	service := &corev1.Service{}
	// ??????service,??????????????????+??????????????????
	err := r.Get(ctx, req.NamespacedName, service)
	// ?????????????????????
	if err == nil {
		logger.Info("Service exists")
		return nil
	}

	// ????????????????????????
	if !errors.IsNotFound(err) {
		logger.Error(err, "query service error")
		return err
	}

	// ????????????,??????Spec
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     PROTOCOL,
					Port:     CONTAINER_PORT,
					NodePort: *instance.Spec.Port,
				},
			},
			Selector: map[string]string{
				"app": APP_NAME,
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}

	// ????????????
	logger.Info("set reference")
	err = controllerutil.SetControllerReference(instance, service, r.Scheme)
	if err != nil {
		logger.Error(err, "set reference for service failed")
		return err
	}

	// ??????service

	logger.Info("create service")
	err = r.Create(ctx, service)
	if err != nil {
		logger.Error(err, "create service error")
		return err
	}

	logger.Info("create service success")
	return nil
}

func (r *ElasticWebReconciler) createDeploymentIfNotExist(ctx context.Context, instance *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	logger := log.FromContext(ctx)
	logger.WithValues("func", "createDeploymentIfNotExist")

	expectReplicas := getExpectReplicas(instance)
	logger.Info(fmt.Sprintf("expected replicas is [%d]", expectReplicas))

	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
		Spec: v1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(expectReplicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": APP_NAME,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": APP_NAME,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            APP_NAME,
							Image:           instance.Spec.Image,
							ImagePullPolicy: "IfNotPresent",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolSCTP,
									ContainerPort: CONTAINER_PORT,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse(CPU_REQUEST),
									"memory": resource.MustParse(MEM_REQUEST),
								},
								Limits: map[corev1.ResourceName]resource.Quantity{
									"cpu":    resource.MustParse(CPU_LIMIT),
									"memory": resource.MustParse(MEM_LIMIT),
								},
							},
						},
					},
				},
			},
		},
	}
	// set reference
	logger.Info("set reference for deployment")
	err := controllerutil.SetControllerReference(instance, deployment, r.Scheme)
	if err != nil {
		logger.Error(err, "set reference for deployment failed")
		return err
	}

	logger.Info("start create deployment")
	if err := r.Create(ctx, deployment); err != nil {
		logger.Error(err, "create deployment error")
		return err
	}

	logger.Info("create deployment successfully")
	return nil
}

func (r *ElasticWebReconciler) updateStatus(ctx context.Context, instance *elasticwebv1.ElasticWeb) error {
	logger := log.FromContext(ctx)
	logger.WithValues("func", "updateStatus")
	// ??????pod???QPS
	singlePodQPS := *(instance.Spec.SinglePodQPS)

	// pod??????
	replicas := getExpectReplicas(instance)

	// ???pod???????????????????????????????????????QPS?????????pod???QPS * pod??????
	// ??????????????????????????????????????????????????????
	if nil == instance.Status.RealQPS {
		instance.Status.RealQPS = new(int32)
	}

	*(instance.Status.RealQPS) = singlePodQPS * replicas

	logger.Info(fmt.Sprintf("singlePodQPS [%d], replicas [%d], realQPS[%d]", singlePodQPS, replicas, *(instance.Status.RealQPS)))

	if err := r.Update(ctx, instance); err != nil {
		logger.Error(err, "update instance error")
		return err
	}

	return nil

}

const (
	PROTOCOL       = "http"
	APP_NAME       = "elastic-app"
	CONTAINER_PORT = 8080
	CPU_REQUEST    = "100m"
	MEM_REQUEST    = "100Mi"
	CPU_LIMIT      = "100m"
	MEM_LIMIT      = "100Mi"
)

func getExpectReplicas(elasticWeb *elasticwebv1.ElasticWeb) int32 {
	// ??????pod???QPS
	singlePodQPS := *(elasticWeb.Spec.SinglePodQPS)

	// ????????????QPS
	totalQPS := *(elasticWeb.Spec.TotalQPS)

	// Replicas???????????????????????????
	replicas := totalQPS / singlePodQPS

	if totalQPS%singlePodQPS > 0 {
		replicas++
	}
	return replicas
}
