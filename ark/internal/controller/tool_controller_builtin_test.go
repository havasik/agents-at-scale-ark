/* Copyright 2025. McKinsey & Company */

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	arkv1alpha1 "mckinsey.com/ark/api/v1alpha1"
	"mckinsey.com/ark/internal/genai"
)

var _ = Describe("Built-in Tool Initialization", func() {
	Context("When initializing built-in tools", func() {
		ctx := context.Background()

		BeforeEach(func() {
			// Clean up any existing built-in tools
			for _, toolName := range []string{genai.BuiltinToolNoop, genai.BuiltinToolTerminate} {
				tool := &arkv1alpha1.Tool{}
				key := types.NamespacedName{Name: toolName, Namespace: "default"}
				if err := k8sClient.Get(ctx, key, tool); err == nil {
					Expect(k8sClient.Delete(ctx, tool)).To(Succeed())
				}
			}
		})

		It("should create all built-in tool CRDs when they don't exist", func() {
			initializer := &builtinToolInitializer{client: k8sClient}

			By("Starting the built-in tool initializer")
			err := initializer.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying all built-in tools were created with correct properties")
			expectedTools := []string{genai.BuiltinToolNoop, genai.BuiltinToolTerminate}

			for _, toolName := range expectedTools {
				tool := &arkv1alpha1.Tool{}
				key := types.NamespacedName{Name: toolName, Namespace: "default"}
				err = k8sClient.Get(ctx, key, tool)
				Expect(err).NotTo(HaveOccurred())
				Expect(tool.Spec.Type).To(Equal(arkv1alpha1.ToolTypeBuiltin))
				Expect(tool.Spec.Description).NotTo(BeEmpty())
				Expect(tool.Labels["ark.mckinsey.com/builtin"]).To(Equal("true"))
			}
		})

		It("should be idempotent and not recreate existing built-in tools", func() {
			initializer := &builtinToolInitializer{client: k8sClient}

			By("Creating the built-in tools first time")
			err := initializer.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			By("Recording original creation times")
			originalCreationTimes := make(map[string]any)
			expectedTools := []string{genai.BuiltinToolNoop, genai.BuiltinToolTerminate}

			for _, toolName := range expectedTools {
				tool := &arkv1alpha1.Tool{}
				key := types.NamespacedName{Name: toolName, Namespace: "default"}
				err = k8sClient.Get(ctx, key, tool)
				Expect(err).NotTo(HaveOccurred())
				originalCreationTimes[toolName] = tool.CreationTimestamp
			}

			By("Running the initializer again")
			err = initializer.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no tools were recreated")
			for _, toolName := range expectedTools {
				tool := &arkv1alpha1.Tool{}
				key := types.NamespacedName{Name: toolName, Namespace: "default"}
				err = k8sClient.Get(ctx, key, tool)
				Expect(err).NotTo(HaveOccurred())
				Expect(tool.CreationTimestamp).To(Equal(originalCreationTimes[toolName]))
			}
		})
	})
})
