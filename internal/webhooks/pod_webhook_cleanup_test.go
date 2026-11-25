// Copyright 2026 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestTerminatingPodCleanupAdmission(t *testing.T) {
	for _, test := range []struct {
		name            string
		operation       admissionv1.Operation
		oldTerminating  bool
		newTerminating  bool
		uninjected      bool
		differentUID    bool
		emptyUIDs       bool
		shorterDeletion bool
		missingOld      bool
		malformedNew    bool
		malformedOld    bool
		withCR          bool
		allowed         bool
	}{
		{name: "remove finalizer after CR deletion", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, allowed: true},
		{name: "do not inject a terminating uninjected pod", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, uninjected: true, allowed: true},
		{name: "shorter deletion grace period remains allowed", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, shorterDeletion: true, allowed: true},
		{name: "active create still requires CR", operation: admissionv1.Create},
		{name: "active injected update still requires CR", operation: admissionv1.Update},
		{name: "create cannot bypass CR lookup with deletion timestamp", operation: admissionv1.Create,
			newTerminating: true},
		{name: "create cannot use old object to bypass CR lookup", operation: admissionv1.Create,
			oldTerminating: true, newTerminating: true},
		{name: "new deletion timestamp cannot bypass CR lookup", operation: admissionv1.Update,
			newTerminating: true},
		{name: "different UID cannot bypass CR lookup", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, differentUID: true},
		{name: "empty UIDs cannot bypass CR lookup", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, emptyUIDs: true},
		{name: "removed deletion timestamp cannot bypass CR lookup", operation: admissionv1.Update,
			oldTerminating: true},
		{name: "missing old pod is rejected", operation: admissionv1.Update,
			newTerminating: true, missingOld: true, withCR: true},
		{name: "malformed new pod is rejected", operation: admissionv1.Update,
			oldTerminating: true, newTerminating: true, malformedNew: true, withCR: true},
		{name: "malformed old pod is rejected", operation: admissionv1.Update,
			newTerminating: true, malformedOld: true, withCR: true},
		{name: "normal injected update with existing CR remains allowed", operation: admissionv1.Update,
			withCR: true, allowed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := v1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			const namespace = "cleanup-test"
			const modelName = "cleanup-model"
			objects := []client.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}}
			if test.withCR {
				objects = append(objects, &v1alpha1.ModelValidation{
					ObjectMeta: metav1.ObjectMeta{Name: modelName, Namespace: namespace},
				})
			}
			handler := NewPodInterceptor(fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
				admission.NewDecoder(scheme), true)
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cleanup-pod", Namespace: namespace, UID: "original-pod",
					Labels:     map[string]string{constants.ModelValidationLabel: modelName},
					Finalizers: []string{constants.ModelValidationFinalizer},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "workload", Image: "busybox"}},
					InitContainers: []corev1.Container{{Name: constants.ModelValidationInitContainerName,
						Image: "validation-agent"}},
				},
			}
			if test.uninjected {
				oldPod.Spec.InitContainers = nil
			}
			deletionTime := metav1.NewTime(time.Unix(1_700_000_000, 0))
			if test.oldTerminating {
				oldPod.DeletionTimestamp = &deletionTime
			}
			pod := oldPod.DeepCopy()
			pod.Finalizers = nil
			pod.DeletionTimestamp = nil
			if test.newTerminating {
				pod.DeletionTimestamp = &deletionTime
			}
			if test.shorterDeletion {
				shorterTime := metav1.NewTime(deletionTime.Add(-time.Second))
				pod.DeletionTimestamp = &shorterTime
			}
			if test.emptyUIDs {
				oldPod.UID, pod.UID = "", ""
			}
			if test.differentUID {
				pod.UID = "different-pod"
			}
			oldBody, err := json.Marshal(oldPod)
			if err != nil {
				t.Fatal(err)
			}
			if test.missingOld {
				oldBody = nil
			}
			if test.malformedOld {
				oldBody = []byte(`{"invalid`)
			}
			body, err := json.Marshal(pod)
			if err != nil {
				t.Fatal(err)
			}
			if test.malformedNew {
				body = []byte(`{"invalid`)
			}
			response := handler.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				UID: "admission-request", Namespace: namespace, Operation: test.operation,
				Object: runtime.RawExtension{Raw: body}, OldObject: runtime.RawExtension{Raw: oldBody},
			}})
			if response.Allowed != test.allowed {
				t.Fatalf("allowed = %t, want %t; result: %+v", response.Allowed, test.allowed, response.Result)
			}
			if len(response.Patches) != 0 {
				t.Fatalf("unexpected mutation of cleanup/existing injected pod: %+v", response.Patches)
			}
			if !test.allowed && (response.Result == nil || response.Result.Code != http.StatusBadRequest) {
				t.Fatalf("expected bad request for missing CR or invalid Pod data, got %+v", response.Result)
			}
		})
	}
}
