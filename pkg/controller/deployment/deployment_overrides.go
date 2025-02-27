package deployment

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	coreinformersv1 "k8s.io/client-go/informers/core/v1"

	v1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/operator-lib/proxy"

	certmanagerinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const (
	// trustedCAVolumeName is the name of the volume with the CA bundle to be trusted by the controller.
	trustedCAVolumeName = "trusted-ca"
	// trustedCAPath is the mounting path for the trusted CA bundle.
	// Default certificate path is taken from the golang source:
	// https://cs.opensource.google/go/go/+/refs/tags/go1.19.5:src/crypto/x509/root_linux.go;drc=82f09b75ca181a6be0e594e1917e4d3d91934b27;l=20
	trustedCAPath = "/etc/pki/tls/certs/cert-manager-tls-ca-bundle.crt"
	// defaultCABundleKey is the default name for the data key of the configmap injected with the trusted CA.
	defaultCABundleKey = "ca-bundle.crt"
)

// overrideArgsFunc defines a function signature that is accepted by
// withContainerArgsOverrideHook(). This function returns the
// override args provided to the cert-manager-operator operator spec.
type overrideArgsFunc func(certmanagerinformer.CertManagerInformer, string) ([]string, error)

// overrideArgsFunc defines a function signature that is accepted by
// withContainerEnvOverrideHook(). This function returns the
// override env provided to the cert-manager-operator operator spec.
type overrideEnvFunc func(certmanagerinformer.CertManagerInformer, string) ([]corev1.EnvVar, error)

// withOperandImageOverrideHook overrides the deployment image with
// the operand images provided to the operator.
func withOperandImageOverrideHook(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	for index := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[index].Image = certManagerImage(deployment.Spec.Template.Spec.Containers[index].Image)
	}
	return nil
}

// withContainerArgsOverrideHook overrides the container args with those provided by
// the overrideArgsFunc function.
func withContainerArgsOverrideHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string, fn overrideArgsFunc) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		overrideArgs, err := fn(certmanagerinformer, deploymentName)
		if err != nil {
			return err
		}

		if overrideArgs != nil && len(overrideArgs) > 0 && len(deployment.Spec.Template.Spec.Containers) == 1 && deployment.Name == deploymentName {
			deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
				deployment.Spec.Template.Spec.Containers[0].Args, overrideArgs)
			sort.Strings(deployment.Spec.Template.Spec.Containers[0].Args)
		}
		return nil
	}
}

// withContainerEnvOverrideHook verrides the container env with those provided by
// the overrideEnvFunc function.
func withContainerEnvOverrideHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string, fn overrideEnvFunc) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		overrideEnv, err := fn(certmanagerinformer, deploymentName)
		if err != nil {
			return err
		}

		if overrideEnv != nil && len(overrideEnv) > 0 && len(deployment.Spec.Template.Spec.Containers) == 1 && deployment.Name == deploymentName {
			deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(
				deployment.Spec.Template.Spec.Containers[0].Env, overrideEnv)

		}
		return nil
	}
}

// withProxyEnv patches the operand deployment if operator
// has proxy variables set. Sets HTTPS_PROXY, HTTP_PROXY and NO_PROXY.
func withProxyEnv(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(deployment.Spec.Template.Spec.Containers[0].Env, proxy.ReadProxyVarsFromEnv())
	return nil
}

// withCAConfigMap patches the operand deployment to include the custom
// ca bundle as a volume. This is set when a trusted ca configmap is provided.
func withCAConfigMap(configmapinformer coreinformersv1.ConfigMapInformer, deployment *appsv1.Deployment, trustedCAConfigmapName string) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {

		if len(trustedCAConfigmapName) == 0 {
			return nil
		}

		_, err := configmapinformer.Lister().ConfigMaps(operatorclient.TargetNamespace).Get(trustedCAConfigmapName)
		if err != nil && apierrors.IsNotFound(err) {
			return fmt.Errorf("(Retrying) trusted CA config map %q doesn't exist due to %v", trustedCAConfigmapName, err)
		} else if err != nil {
			return err
		}

		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: trustedCAVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: trustedCAConfigmapName,
					},
				},
			},
		})

		for i := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name:      trustedCAVolumeName,
				MountPath: trustedCAPath,
				SubPath:   defaultCABundleKey,
			})
		}

		return nil
	}
}
