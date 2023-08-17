/*
 *
 * Copyright 2023 keington.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 * /
 */

package utils

import (
	"context"
	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	serviceType corev1.ServiceType
)

func serviceLogger(namespace string, name string) logr.Logger {
	reqLogger := log.WithValues("Request.Service.Namespace", namespace, "Request.Service.Name", name)
	return reqLogger
}

// generateServiceType generates service type
func generateServiceType(k8sServiceType string) corev1.ServiceType {
	switch k8sServiceType {
	case "LoadBalancer":
		serviceType = corev1.ServiceTypeLoadBalancer
	case "NodePort":
		serviceType = corev1.ServiceTypeNodePort
	case "ClusterIP":
		serviceType = corev1.ServiceTypeClusterIP
	default:
		serviceType = corev1.ServiceTypeClusterIP
	}
	return serviceType
}

// createService is a method to create service is Kubernetes
func createService(namespace string, service *corev1.Service) error {
	logger := serviceLogger(namespace, service.Name)
	_, err := createKubernetesClient().CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		logger.Error(err, "Redis service creation is failed")
		return err
	}
	logger.Info("Redis service creation is successful")
	return nil
}

// updateService is a method to update service is Kubernetes
func updateService(namespace string, service *corev1.Service) error {
	logger := serviceLogger(namespace, service.Name)
	_, err := createKubernetesClient().CoreV1().Services(namespace).Update(context.TODO(), service, metav1.UpdateOptions{})
	if err != nil {
		logger.Error(err, "Redis service update failed")
		return err
	}
	logger.Info("Redis service updated successfully")
	return nil
}

// getService is a method to get service is Kubernetes
func getService(namespace string, service string) (*corev1.Service, error) {
	logger := serviceLogger(namespace, service)
	getOpts := metav1.GetOptions{
		TypeMeta: generateMetaInformation("Service", "v1"),
	}
	serviceInfo, err := createKubernetesClient().CoreV1().Services(namespace).Get(context.TODO(), service, getOpts)
	if err != nil {
		logger.Info("Redis service get action is failed")
		return nil, err
	}
	logger.Info("Redis service get action is successful")
	return serviceInfo, nil
}

// generateServiceDef generates service definition for Redis
func generateServiceDef(serviceMeta metav1.ObjectMeta, ownerDef metav1.OwnerReference, headless bool, serviceType string) *corev1.Service {
	var PortName string
	var PortNum int32
	if serviceMeta.Labels["role"] == "sentinel" {
		PortName = "sentinel-client"
	} else {
		PortName = "redis-client"
	}
	service := &corev1.Service{
		TypeMeta:   generateMetaInformation("Service", "v1"),
		ObjectMeta: serviceMeta,
		Spec: corev1.ServiceSpec{
			Type:      generateServiceType(serviceType),
			ClusterIP: "",
			Selector:  serviceMeta.GetLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       PortName,
					Port:       PortNum,
					TargetPort: intstr.FromInt(int(PortNum)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	if headless {
		service.Spec.ClusterIP = "None"
	}
	AddOwnerRefToObject(service, ownerDef)
	return service
}

// CreateOrUpdateService method will create or update Redis service
func CreateOrUpdateService(namespace string, serviceMeta metav1.ObjectMeta, ownerDef metav1.OwnerReference, headless bool, serviceType string) error {
	logger := serviceLogger(namespace, serviceMeta.Name)
	serviceDef := generateServiceDef(serviceMeta, ownerDef, headless, serviceType)
	storedService, err := getService(namespace, serviceMeta.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(serviceDef); err != nil {
				logger.Error(err, "Unable to patch redis service with compare annotations")
			}
			return createService(namespace, serviceDef)
		}
		return err
	}
	return patchService(storedService, serviceDef, namespace)
}

// patchService will patch Redis Kubernetes service
func patchService(storedService *corev1.Service, newService *corev1.Service, namespace string) error {
	logger := serviceLogger(namespace, storedService.Name)
	// We want to try and keep this atomic as possible.
	newService.ResourceVersion = storedService.ResourceVersion
	newService.CreationTimestamp = storedService.CreationTimestamp
	newService.ManagedFields = storedService.ManagedFields

	if newService.Spec.Type == generateServiceType("ClusterIP") {
		newService.Spec.ClusterIP = storedService.Spec.ClusterIP
	}

	patchResult, err := patch.DefaultPatchMaker.Calculate(storedService, newService,
		patch.IgnoreStatusFields(),
		patch.IgnoreField("kind"),
		patch.IgnoreField("apiVersion"),
	)
	if err != nil {
		logger.Error(err, "Unable to patch redis service with comparison object")
		return err
	}
	if !patchResult.IsEmpty() {
		logger.Info("Changes in service Detected, Updating...", "patch", string(patchResult.Patch))

		for key, value := range storedService.Annotations {
			if _, present := newService.Annotations[key]; !present {
				newService.Annotations[key] = value
			}
		}
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newService); err != nil {
			logger.Error(err, "Unable to patch redis service with comparison object")
			return err
		}
		logger.Info("Syncing Redis service with defined properties")
		return updateService(namespace, newService)
	}
	logger.Info("Redis service is already in-sync")
	return nil
}