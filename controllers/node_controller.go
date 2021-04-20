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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cloudprovider "k8s.io/cloud-provider"

	// a bit nervous about importing something that says "legacy"...
	//"k8s.io/legacy-cloud-providers/aws"
	"errors"
)

const (
	deleteNodeEvent = "DeletingNode"
)

// NodeReconciler reconciles a Node object
type NodeReconciler struct {
	client.Client
	Recorder record.EventRecorder
	CloudInstances cloudprovider.Instances
	Log    logr.Logger
	Scheme *runtime.Scheme
	DryRun bool
}

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=nodes/finalizers,verbs=update

// Recursively check the list of nodes for any nodes that need to be removed from the cluster
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("node", req.NamespacedName)

	// your logic here
	node := &corev1.Node{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, node)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			logger.V(1).Info("Node deleted while performing reconciliation step")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err,"Error fetching Node object from api client")
		return reconcile.Result{}, err
	}

	// filter out nodes without a ProviderID, since we can't fact check them against the cloud provider
	if node.Spec.ProviderID != "" {
		status, err := getNodeReadyCondition(node.Status.Conditions)
		if err != nil {
			logger.Error(err, "Something has gone horribly wrong.")
		}

		// Operate on nodes that are not ready (ready=false) or conspicuously missing (ready=unknown)
		switch status.Status {
		case corev1.ConditionFalse:
		case corev1.ConditionUnknown:
			logger.V(1).Info("Node appears down according to APIServer, investigating", status.Status)

			ref := &corev1.ObjectReference{
				Kind:      "Node",
				Name:      node.Name,
				UID:       node.UID,
				Namespace: "",
			}

			nodeExists, err := r.CloudInstances.InstanceExistsByProviderID(context.TODO(), node.Spec.ProviderID)
			nodeShutdown, err := r.CloudInstances.InstanceShutdownByProviderID(context.TODO(), node.Spec.ProviderID)
			shouldDelete := !nodeExists || nodeShutdown
			if err != nil {
				logger.Error(err, "Error while fetching node status")
			}

			if !nodeExists {
				logger.Info("Deleting node since it is no longer present in cloud provider")

				r.Recorder.Eventf(ref, corev1.EventTypeNormal, deleteNodeEvent,
					"Deleting node %s because it does not exist in the cloud provider", node.Name)
			}

			if nodeShutdown {
				logger.Info("Node is shutdown, deleting node")

				r.Recorder.Eventf(ref, corev1.EventTypeNormal, deleteNodeEvent,
					"Deleting node %s because it is shut down in the cloud provider", node.Name)
			}

			// Nuke 'em, captain.
			if shouldDelete {
				if !r.DryRun {
					err := r.Client.Delete(context.TODO(), node)
					if err != nil {
						logger.Error(err, "Unable to delete node")
					}
				} else {
					logger.Info("Would have deleted node")
				}
			}
		}

	} else {
		logger.V(1).Info("Node has no ProviderID, skipping")
	}

	return ctrl.Result{}, nil
}

// Filter to only the NodeReady condition
func getNodeReadyCondition(status []corev1.NodeCondition) (corev1.NodeCondition, error) {
	for _, condition := range status {
		if condition.Type == corev1.NodeReady {
			return condition, nil
		}
	}
	return corev1.NodeCondition{}, errors.New("unable to find NodeReady condition. something is wrong, bruh.")
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
