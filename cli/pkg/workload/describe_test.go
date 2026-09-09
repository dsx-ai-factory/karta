// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

package workload

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/run-ai/karta/cli/pkg/definitions"
	"github.com/run-ai/karta/pkg/catalog"
)

// describeFixture resolves a manifest from testdata through the built-in
// catalog, the way the describe command resolves a live object.
func describeFixture(name string, pods ...corev1.Pod) *DescribeView {
	GinkgoHelper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	Expect(err).NotTo(HaveOccurred())

	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(raw, obj)).To(Succeed())

	def, err := definitions.New(catalog.List(), nil).Resolve(obj.GroupVersionKind())
	Expect(err).NotTo(HaveOccurred())

	view, err := ResolveDescribe(context.Background(), obj, def, pods)
	Expect(err).NotTo(HaveOccurred())
	return view
}

// componentNamed finds a component at any depth, so a test does not have to
// spell out the path through grouping components.
func componentNamed(components []ComponentView, name string) *ComponentView {
	for i := range components {
		if components[i].Name == name {
			return &components[i]
		}
		if found := componentNamed(components[i].Children, name); found != nil {
			return found
		}
	}
	return nil
}

// livePod builds a scheduled, ready pod carrying labels a PodSelector matches.
func livePod(name, node string, labels map[string]string, gpus string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ml-team", Labels: labels},
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{gpuResourceName: resourceQuantity(gpus)},
				},
			}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

// unschedulablePod is a pod that never got a node, so its node reads as null.
func unschedulablePod(name string, labels map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ml-team", Labels: labels},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable",
			}},
		},
	}
}

func masterLabels() map[string]string {
	return map[string]string{"training.kubeflow.org/replica-type": "master"}
}

func workerLabels() map[string]string {
	return map[string]string{"training.kubeflow.org/replica-type": "worker"}
}

var _ = Describe("ResolveDescribe", func() {
	Context("without pods, as file mode builds it", func() {
		It("carries the structure, the desired scale and the requested resources", func() {
			view := describeFixture("pytorchjob.yaml")

			Expect(view.Name).To(Equal("llama-finetune"))
			Expect(view.Kind).To(Equal("PyTorchJob"))
			Expect(view.Definition).To(Equal("kubeflow-org-pytorchjob-v1"))
			Expect(view.Origin).To(Equal(string(definitions.OriginCatalog)))

			master := componentNamed(view.Components, "master")
			Expect(master).NotTo(BeNil())
			Expect(master.Replicas).To(Equal(Replicas{Desired: 1}))
			Expect(master.Resources.GPUs).To(Equal(int64(1)))

			worker := componentNamed(view.Components, "worker")
			Expect(worker.Replicas).To(Equal(Replicas{Desired: 4}))
			Expect(worker.Resources.GPUs).To(Equal(int64(32)), "8 per replica across 4 replicas")

			Expect(view.Resources.GPUs).To(Equal(int64(33)))
		})

		It("leaves every live field empty rather than reporting zeroes as facts", func() {
			view := describeFixture("pytorchjob.yaml")

			for _, component := range view.Components {
				Expect(component.Pods).To(BeEmpty())
				Expect(component.Nodes).To(BeEmpty())
				Expect(component.Replicas.Current).To(BeZero())
				Expect(component.Replicas.Ready).To(BeZero())
			}
		})
	})

	Context("with live pods", func() {
		It("attributes each pod to the component its selector names", func() {
			view := describeFixture("pytorchjob.yaml",
				livePod("llama-finetune-master-0", "node-01", masterLabels(), "1"),
				livePod("llama-finetune-worker-1", "node-03", workerLabels(), "8"),
				livePod("llama-finetune-worker-0", "node-02", workerLabels(), "8"),
				livePod("llama-finetune-worker-2", "node-04", workerLabels(), "8"),
				unschedulablePod("llama-finetune-worker-3", workerLabels()),
			)

			master := componentNamed(view.Components, "master")
			Expect(master.Replicas).To(Equal(Replicas{Desired: 1, Current: 1, Ready: 1}))
			Expect(master.Nodes).To(Equal([]string{"node-01"}))

			worker := componentNamed(view.Components, "worker")
			Expect(worker.Replicas).To(Equal(Replicas{Desired: 4, Current: 4, Ready: 3}))
			Expect(worker.Nodes).To(Equal([]string{"node-02", "node-03", "node-04"}))

			names := make([]string, 0, len(worker.Pods))
			for _, pod := range worker.Pods {
				names = append(names, pod.Name)
			}
			Expect(names).To(Equal([]string{
				"llama-finetune-worker-0", "llama-finetune-worker-1",
				"llama-finetune-worker-2", "llama-finetune-worker-3",
			}), "pod rows are name-ordered, so a re-run reads the same")
		})

		It("reports an unscheduled pod as a null node with its reason", func() {
			view := describeFixture("pytorchjob.yaml",
				unschedulablePod("llama-finetune-worker-3", workerLabels()))

			pending := componentNamed(view.Components, "worker").Pods[0]
			Expect(pending.Node).To(BeNil())
			Expect(pending.Ready).To(BeFalse())
			Expect(pending.Phase).To(Equal("Pending"))
			Expect(pending.Reason).To(Equal("Unschedulable"))
		})

		// The selector says which role a pod plays, so a worker pod must not
		// land under master just because both belong to the workload.
		It("does not claim a pod for a component whose selector rejects it", func() {
			view := describeFixture("pytorchjob.yaml",
				livePod("llama-finetune-worker-0", "node-02", workerLabels(), "8"))

			Expect(componentNamed(view.Components, "master").Pods).To(BeEmpty())
			Expect(componentNamed(view.Components, "worker").Pods).To(HaveLen(1))
		})

		// Requested resources come from the spec, so they do not move when a
		// replica is missing; only the counts do.
		It("keeps the requested totals independent of how many pods exist", func() {
			withPods := describeFixture("pytorchjob.yaml",
				livePod("llama-finetune-master-0", "node-01", masterLabels(), "1"))

			Expect(withPods.Resources.GPUs).To(Equal(describeFixture("pytorchjob.yaml").Resources.GPUs))
		})
	})

	// tree.Build drops the root, so a Deployment's own pod template is only
	// visible to a consumer that asks the root component for it.
	It("renders a workload whose pod template lives on the root", func() {
		view := describeFixture("deployment.yaml")

		Expect(view.Components).To(HaveLen(1))
		Expect(view.Components[0].Name).To(Equal("deployment"))
		Expect(view.Components[0].Replicas.Desired).To(Equal(int32(3)))
		Expect(view.Resources.GPUs).To(Equal(int64(6)), "a limit counts when no request is set")
	})

	// The Deployment definition names a ReplicaSet child that carries no pods,
	// no scale and no descendants of its own.
	It("collapses an intermediate component that carries nothing", func() {
		Expect(componentNamed(describeFixture("deployment.yaml").Components, "replicaset")).To(BeNil())
	})

	// Scaled to zero is a state a workload is deliberately in, not plumbing the
	// definition named and the workload never used.
	It("keeps a pod-bearing component scaled to zero", func() {
		view := describeObject([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: embed-svc
  namespace: ml-team
spec:
  replicas: 0
  template:
    spec:
      containers:
        - name: server
`))

		Expect(view.Components).To(HaveLen(1))
		Expect(view.Components[0].Name).To(Equal("deployment"))
		Expect(view.Components[0].Replicas).To(Equal(Replicas{}))
	})

	// Each child is named by its instance key, not by the component.
	It("splits a multi-instance component into one child per instance", func() {
		view := describeFixture("dynamographdeployment.yaml")

		frontend := componentNamed(view.Components, "Frontend")
		Expect(frontend).NotTo(BeNil())
		Expect(frontend.Replicas.Desired).To(Equal(int32(2)))
		Expect(frontend.Resources.GPUs).To(Equal(int64(2)))

		prefill := componentNamed(view.Components, "PrefillWorker")
		Expect(prefill.Replicas.Desired).To(Equal(int32(4)))
		Expect(prefill.Resources.GPUs).To(Equal(int64(32)))

		Expect(view.Resources.GPUs).To(Equal(int64(34)))
	})

	It("sums cpu as millicores and memory as bytes", func() {
		view := describeObject([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: embed-svc
  namespace: ml-team
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: server
          resources:
            requests:
              cpu: "500m"
              memory: "1Gi"
`))

		Expect(view.Resources.CPUMillis).To(Equal(int64(1000)))
		Expect(view.Resources.MemoryBytes).To(Equal(int64(2 * 1024 * 1024 * 1024)))
	})
})

// describeObject resolves an inline manifest through the built-in catalog.
func describeObject(manifest []byte) *DescribeView {
	GinkgoHelper()

	obj := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(manifest, obj)).To(Succeed())

	def, err := definitions.New(catalog.List(), nil).Resolve(obj.GroupVersionKind())
	Expect(err).NotTo(HaveOccurred())

	view, err := ResolveDescribe(context.Background(), obj, def, nil)
	Expect(err).NotTo(HaveOccurred())
	return view
}

// resourceQuantity parses a quantity a fixture spells as a string.
func resourceQuantity(value string) apiresource.Quantity {
	GinkgoHelper()
	quantity, err := apiresource.ParseQuantity(value)
	Expect(err).NotTo(HaveOccurred())
	return quantity
}
