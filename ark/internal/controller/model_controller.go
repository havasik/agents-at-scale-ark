/* Copyright 2025. McKinsey & Company */

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	arkv1alpha1 "mckinsey.com/ark/api/v1alpha1"
	"mckinsey.com/ark/internal/genai"
)

const (
	// Condition types
	ModelReady       = "Ready"
	ModelDiscovering = "Discovering"
)

type ModelReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=models,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=models/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=models/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *ModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var model arkv1alpha1.Model
	if err := r.Get(ctx, req.NamespacedName, &model); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch model", "model", req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Initialize conditions if empty
	if len(model.Status.Conditions) == 0 {
		r.setCondition(&model, ModelReady, metav1.ConditionFalse, "Initializing", "Model is being initialized")
		r.setCondition(&model, ModelDiscovering, metav1.ConditionTrue, "StartingValidation", "Starting model validation process")
		if err := r.updateStatus(ctx, &model); err != nil {
			return ctrl.Result{}, err
		}
		// Return early to avoid double reconciliation, let the status update trigger next reconcile
		return ctrl.Result{}, nil
	}

	return r.processModel(ctx, model)
}

func (r *ModelReconciler) processModel(ctx context.Context, model arkv1alpha1.Model) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	logf.FromContext(ctx).Info("model validation started", "model", model.Name, "namespace", model.Namespace)

	// Set discovering condition to true when starting validation
	r.setCondition(&model, ModelDiscovering, metav1.ConditionTrue, "ValidatingModel", "Validating model configuration and connectivity")

	// Process model resolution
	recorder := genai.NewModelRecorder(&model, r.Recorder)

	// Create operation tracker for model resolution
	modelTracker := genai.NewOperationTracker(recorder, ctx, "ModelResolve", model.Name, map[string]string{
		"namespace": model.Namespace,
		"modelName": model.Spec.Model.Value,
	})

	if err := r.validateModel(ctx, model); err != nil {
		log.Error(err, "model validation failed", "model", model.Name)
		modelTracker.Fail(err)
		r.setCondition(&model, ModelReady, metav1.ConditionFalse, "ModelResolutionFailed", err.Error())
		r.setCondition(&model, ModelDiscovering, metav1.ConditionFalse, "ValidationFailed", "Model validation failed")
		if err := r.updateStatus(ctx, &model); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: model.Spec.PollInterval.Duration}, nil
	}

	modelTracker.Complete("resolved")
	return r.finalizeModelProcessing(ctx, model)
}

func (r *ModelReconciler) validateModel(ctx context.Context, model arkv1alpha1.Model) error {
	resolvedModel, err := genai.LoadModel(ctx, r.Client, &arkv1alpha1.AgentModelRef{
		Name:      model.Name,
		Namespace: model.Namespace,
	}, model.Namespace)
	if err != nil {
		return err
	}

	validationCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testMessages := []genai.Message{genai.NewUserMessage("Hello")}
	_, err = resolvedModel.ChatCompletion(validationCtx, testMessages, nil)
	return err
}

func (r *ModelReconciler) finalizeModelProcessing(ctx context.Context, model arkv1alpha1.Model) (ctrl.Result, error) {
	r.setCondition(&model, ModelDiscovering, metav1.ConditionFalse, "ValidationComplete", "Model validation completed successfully")
	r.setCondition(&model, ModelReady, metav1.ConditionTrue, "ModelResolved", "Model successfully resolved and validated")
	if err := r.updateStatus(ctx, &model); err != nil {
		return ctrl.Result{}, err
	}

	logf.FromContext(ctx).Info("model validation completed", "model", model.Name, "namespace", model.Namespace)

	// Return with requeue interval for continuous polling
	return ctrl.Result{RequeueAfter: model.Spec.PollInterval.Duration}, nil
}

// setCondition sets a condition on the Model
func (r *ModelReconciler) setCondition(model *arkv1alpha1.Model, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&model.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: model.Generation,
	})
}

// updateStatus updates the Model status
func (r *ModelReconciler) updateStatus(ctx context.Context, model *arkv1alpha1.Model) error {
	if ctx.Err() != nil {
		return nil
	}
	err := r.Status().Update(ctx, model)
	if err != nil {
		logf.FromContext(ctx).Error(err, "failed to update model status")
	}
	return err
}

func (r *ModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arkv1alpha1.Model{}).
		Named("model").
		Complete(r)
}
