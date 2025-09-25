/* Copyright 2025. McKinsey & Company */

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	arkv1alpha1 "mckinsey.com/ark/api/v1alpha1"
	"mckinsey.com/ark/internal/genai"
)

type ToolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=tools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=tools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ark.mckinsey.com,resources=tools/finalizers,verbs=update

func (r *ToolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	tool := &arkv1alpha1.Tool{}
	if err := r.Get(ctx, req.NamespacedName, tool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if tool.Status.State == arkv1alpha1.ToolStateReady {
		return ctrl.Result{}, nil
	}

	return r.updateToolStatus(ctx, tool, arkv1alpha1.ToolStateReady, "Tool configuration is valid")
}

func (r *ToolReconciler) updateToolStatus(ctx context.Context, tool *arkv1alpha1.Tool, state, message string) (ctrl.Result, error) {
	tool.Status.State = state
	tool.Status.Message = message

	if err := r.Status().Update(ctx, tool); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update tool status: %v", err)
	}

	return ctrl.Result{}, nil
}

func (r *ToolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(&builtinToolInitializer{client: mgr.GetClient()}); err != nil {
		return fmt.Errorf("failed to add builtin tool initializer: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).For(&arkv1alpha1.Tool{}).Named("tool").Complete(r)
}

type builtinToolInitializer struct {
	client client.Client
}

func (b *builtinToolInitializer) Start(ctx context.Context) error {
	log := logf.FromContext(ctx)
	log.Info("initializing built-in tools")

	if err := b.ensureBuiltinToolsExist(ctx); err != nil {
		log.Error(err, "failed to create built-in tools")
		return fmt.Errorf("failed to create built-in tools: %w", err)
	}

	log.Info("built-in tools initialization completed")
	return nil
}

func (b *builtinToolInitializer) ensureBuiltinToolsExist(ctx context.Context) error {
	log := logf.FromContext(ctx)
	builtinTools := []struct {
		name        string
		description string
		parameters  map[string]any
	}{
		{
			name:        genai.BuiltinToolNoop,
			description: "A no-operation tool that does nothing and returns success",
			parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{
						"type":        "string",
						"description": "Optional message to include in the response",
					},
				},
			},
		},
		{
			name:        genai.BuiltinToolTerminate,
			description: "Use this function to provide a final response to the user and then end the current conversation",
			parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"response": map[string]any{
						"type":        "string",
						"description": "The message to send before ending the conversation",
					},
				},
				"required": []string{"response"},
			},
		},
	}

	for _, builtinTool := range builtinTools {
		if err := b.ensureSingleBuiltinTool(ctx, log, builtinTool.name, builtinTool.description, builtinTool.parameters); err != nil {
			return err
		}
	}

	return nil
}

func (b *builtinToolInitializer) ensureSingleBuiltinTool(ctx context.Context, log logr.Logger, name, description string, parameters map[string]any) error {
	log.Info("ensuring built-in tool exists", "tool", name)

	tool := &arkv1alpha1.Tool{}
	key := client.ObjectKey{Name: name, Namespace: "default"}

	err := b.client.Get(ctx, key, tool)
	if err == nil {
		log.Info("built-in tool already exists", "tool", name)
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get tool %s: %w", name, err)
	}

	if err := b.createBuiltinTool(ctx, name, description, parameters); err != nil {
		return fmt.Errorf("failed to create built-in tool %s: %w", name, err)
	}

	log.Info("created built-in tool", "tool", name)
	return nil
}

func (b *builtinToolInitializer) createBuiltinTool(ctx context.Context, name, description string, parameters map[string]any) error {
	parametersJSON, err := json.Marshal(parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters: %w", err)
	}

	tool := &arkv1alpha1.Tool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"ark.mckinsey.com/builtin": "true",
			},
		},
		Spec: arkv1alpha1.ToolSpec{
			Type:        arkv1alpha1.ToolTypeBuiltin,
			Description: description,
			InputSchema: &runtime.RawExtension{
				Raw: parametersJSON,
			},
		},
	}

	return b.client.Create(ctx, tool)
}
