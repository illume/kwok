/*
Copyright 2023 The Kubernetes Authors.

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

package helper

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"sigs.k8s.io/kwok/pkg/log"
	"sigs.k8s.io/kwok/pkg/utils/slices"
)

// nodeIsReady returns a function that checks if a node is ready
func nodeIsReady(name string) func(obj k8s.Object) bool {
	return func(obj k8s.Object) bool {
		node := obj.(*corev1.Node)
		if node.Name != name {
			return false
		}
		cond, ok := slices.Find(node.Status.Conditions, func(cond corev1.NodeCondition) bool {
			return cond.Type == corev1.NodeReady
		})
		if ok && cond.Status == corev1.ConditionTrue {
			return true
		}
		return true
	}
}

// CreateNode creates a node and waits for it to be ready
func CreateNode(node *corev1.Node) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("creating node", node.Name)
		err = client.Create(ctx, node)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("waiting for node to be ready", node.Name)
		err = wait.For(
			conditions.New(client).ResourceMatch(node, nodeIsReady(node.Name)),
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("node is ready", node.Name)
		return ctx
	}
}

// DeleteNode deletes a node
func DeleteNode(node *corev1.Node) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("deleting node", node.Name)
		err = client.Delete(ctx, node)
		if err != nil {
			t.Fatal(err)
		}

		err = wait.For(
			conditions.New(client).ResourceDeleted(node),
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
}

// CreatePod creates a pod and waits for it to be ready
func CreatePod(pod *corev1.Pod) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("creating pod", log.KObj(pod))
		err = client.Create(ctx, pod)
		if err != nil {
			t.Fatal(err)
		}

		t.Log("waiting for pod to be ready", log.KObj(pod))
		err = wait.For(
			conditions.New(client).PodConditionMatch(pod, corev1.PodReady, corev1.ConditionTrue),
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		err = client.Get(ctx, pod.GetName(), pod.GetNamespace(), pod)
		if err != nil {
			t.Fatal(err)
		}

		if pod.Spec.NodeName == "" {
			t.Fatal("pod node name is empty", log.KObj(pod))
		}

		if pod.Status.PodIP != "" {
			if pod.Spec.HostNetwork {
				if pod.Status.PodIP != pod.Status.HostIP {
					t.Fatal("pod ip is not equal to host ip", log.KObj(pod))
				}
			} else {
				if pod.Status.PodIP == pod.Status.HostIP {
					t.Fatal("pod ip is equal to host ip", log.KObj(pod))
				}

				var node corev1.Node
				err = client.Get(ctx, pod.Spec.NodeName, "", &node)
				if err != nil {
					t.Fatal(err)
				}

				if node.Spec.PodCIDR != "" {
					_, ipnet, err := net.ParseCIDR(node.Spec.PodCIDR)
					if err != nil {
						t.Fatal(err)
					}

					ip := net.ParseIP(pod.Status.PodIP)
					if !ipnet.Contains(ip) {
						t.Fatal("pod ip is not in pod cidr", log.KObj(pod))
					}
				}
			}
		}

		t.Log("pod is ready", log.KObj(pod))
		return ctx
	}
}

// DeletePod deletes a pod
func DeletePod(pod *corev1.Pod) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("deleting pod", log.KObj(pod))
		err = client.Delete(ctx, pod)
		if err != nil {
			t.Fatal(err)
		}

		err = wait.For(
			conditions.New(client).ResourceDeleted(pod),
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
}

// WaitForAllNodesReady waits for all nodes to be ready
func WaitForAllNodesReady() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			return nil, err
		}

		var list corev1.NodeList
		err = wait.For(
			func(ctx context.Context) (done bool, err error) {
				if err = client.List(ctx, &list); err != nil {
					return false, err
				}
				var found int
				metaList, err := meta.ExtractList(&list)
				if err != nil {
					return false, err
				}
				if len(metaList) == 0 {
					return false, fmt.Errorf("no node found")
				}
				for _, obj := range metaList {
					node := obj.(*corev1.Node)
					cond, ok := slices.Find(node.Status.Conditions, func(cond corev1.NodeCondition) bool {
						return cond.Type == corev1.NodeReady
					})
					if ok && cond.Status == corev1.ConditionTrue {
						found++
					}
				}
				return found == len(metaList), nil
			},
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			return nil, err
		}

		return ctx, nil
	}
}

// WaitForAllPodsReady waits for all pods to be ready
func WaitForAllPodsReady() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		client, err := resources.New(c.Client().RESTConfig())
		if err != nil {
			return nil, err
		}

		var list corev1.PodList
		err = wait.For(
			func(ctx context.Context) (done bool, err error) {
				if err = client.List(ctx, &list); err != nil {
					return false, err
				}
				var found int
				metaList, err := meta.ExtractList(&list)
				if err != nil {
					return false, err
				}
				if len(metaList) == 0 {
					return false, fmt.Errorf("no pod found")
				}
				for _, obj := range metaList {
					pod := obj.(*corev1.Pod)
					if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded {
						found++
					}
				}
				return found == len(metaList), nil
			},
			wait.WithContext(ctx),
			wait.WithTimeout(600*time.Second),
		)
		if err != nil {
			return nil, err
		}

		return ctx, nil
	}
}
