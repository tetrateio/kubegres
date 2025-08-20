/*
Copyright 2021 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

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

package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ----------------------- SPEC -------------------------------------------

// SSLMode honors https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION.
type SSLMode string

const (
	SSLModeDisable    SSLMode = "disable"
	SSLModeAllow      SSLMode = "allow"
	SSLModePrefer     SSLMode = "prefer"
	SSLModeRequire    SSLMode = "require"
	SSLModeVerifyCA   SSLMode = "verify-ca"
	SSLModeVerifyFull SSLMode = "verify-full"
)

var (
	sslModeTransitPriority = [][]SSLMode{
		{SSLModeDisable},
		{SSLModeAllow, SSLModePrefer},
		{SSLModeRequire, SSLModeVerifyCA, SSLModeVerifyFull},
	}
)

func (s SSLMode) String() string {
	return string(s)
}

func (s SSLMode) Priority() int {
	for i, modes := range sslModeTransitPriority {
		for _, mode := range modes {
			if mode == s {
				return i
			}
		}
	}
	return 0 // Default to disable
}

func (s SSLMode) Higher() SSLMode {
	if s.Priority() < len(sslModeTransitPriority)-1 {
		return sslModeTransitPriority[s.Priority()+1][0]
	}
	return s
}

func (s SSLMode) Lower() SSLMode {
	if s.Priority() > 0 {
		return sslModeTransitPriority[s.Priority()-1][0]
	}
	return s
}

type TLS struct {
	// Enabled indicates whether TLS is enabled in Postgresql connections. Defaults to false.
	Enabled bool `json:"enabled,omitempty"`
	// SecretName is the name of the Kubernetes secret that contains the TLS certificates. Required if TLS is enabled.
	SecretName string `json:"secretName,omitempty"`
	// MountPath is the path where the TLS certificates will be mounted in the Pod. Defaults to /var/lib/postgresql/data/tls
	MountPath string `json:"mountPath,omitempty"`
	// RootCertPath is the path to the root certificate file. Defaults to /var/lib/postgresql/data/tls/root.crt
	RootCertPath string `json:"rootCert,omitempty"`
	// ServerCertPath is the path to the server certificate file. Defaults to /var/lib/postgresql/data/tls/server.crt
	ServerCertPath string `json:"serverCert,omitempty"`
	// ServerKeyPath is the path to the server key file. Defaults to /var/lib/postgresql/data/tls/server.key
	ServerKeyPath string `json:"serverKey,omitempty"`
	// ClientCertPath is the path to the client certificate file. Defaults to /var/lib/postgresql/data/tls/client.crt
	ClientCertPath string `json:"clientCert,omitempty"`
	// ClientKeyPath is the path to the client key file. Defaults to /var/lib/postgresql/data/tls/client.key
	ClientKeyPath string `json:"clientKey,omitempty"`
	// SSLMode honors https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION. Required if TLS is enabled.
	SSLMode SSLMode `json:"mode,omitempty"`
}
type KubegresDatabase struct {
	Size             string  `json:"size,omitempty"`
	VolumeMount      string  `json:"volumeMount,omitempty"`
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type KubegresBackUp struct {
	Schedule    string `json:"schedule,omitempty"`
	VolumeMount string `json:"volumeMount,omitempty"`
	PvcName     string `json:"pvcName,omitempty"`
}

type KubegresFailover struct {
	IsDisabled bool   `json:"isDisabled,omitempty"`
	PromotePod string `json:"promotePod,omitempty"`
}

type KubegresScheduler struct {
	Affinity    *v1.Affinity    `json:"affinity,omitempty"`
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
}

type VolumeClaimTemplate struct {
	Name string                       `json:"name,omitempty"`
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

type Volume struct {
	VolumeMounts         []v1.VolumeMount      `json:"volumeMounts,omitempty"`
	Volumes              []v1.Volume           `json:"volumes,omitempty"`
	VolumeClaimTemplates []VolumeClaimTemplate `json:"volumeClaimTemplates,omitempty"`
}

type Probe struct {
	LivenessProbe  *v1.Probe `json:"livenessProbe,omitempty"`
	ReadinessProbe *v1.Probe `json:"readinessProbe,omitempty"`
}

type KubegresSpec struct {
	Replicas           *int32                    `json:"replicas,omitempty"`
	Image              string                    `json:"image,omitempty"`
	Port               int32                     `json:"port,omitempty"`
	ImagePullSecrets   []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	CustomConfig       string                    `json:"customConfig,omitempty"`
	Database           KubegresDatabase          `json:"database,omitempty"`
	Failover           KubegresFailover          `json:"failover,omitempty"`
	Backup             KubegresBackUp            `json:"backup,omitempty"`
	Env                []v1.EnvVar               `json:"env,omitempty"`
	Scheduler          KubegresScheduler         `json:"scheduler,omitempty"`
	Resources          v1.ResourceRequirements   `json:"resources,omitempty"`
	Volume             Volume                    `json:"volume,omitempty"`
	SecurityContext    *v1.PodSecurityContext    `json:"securityContext,omitempty"`
	Probe              Probe                     `json:"probe,omitempty"`
	ServiceAccountName string                    `json:"serviceAccountName,omitempty"`
	Standby            Standby                   `json:"standby,omitempty"`
	SidecarContainers  []v1.Container            `json:"sidecarContainers,omitempty"`
	TLS                TLS                       `json:"tls,omitempty"`
}

type Standby struct {
	Enabled         bool   `json:"enabled,omitempty"`
	PrimaryEndpoint string `json:"primaryEndpoint,omitempty"`
}

// ----------------------- STATUS -----------------------------------------

type KubegresStatefulSetOperation struct {
	InstanceIndex int32  `json:"instanceIndex,omitempty"`
	Name          string `json:"name,omitempty"`
}

type KubegresStatefulSetSpecUpdateOperation struct {
	SpecDifferences string `json:"specDifferences,omitempty"`
}

type KubegresBlockingOperation struct {
	OperationId          string `json:"operationId,omitempty"`
	StepId               string `json:"stepId,omitempty"`
	TimeOutEpocInSeconds int64  `json:"timeOutEpocInSeconds,omitempty"`
	HasTimedOut          bool   `json:"hasTimedOut,omitempty"`

	// Custom operation fields
	StatefulSetOperation           KubegresStatefulSetOperation           `json:"statefulSetOperation,omitempty"`
	StatefulSetSpecUpdateOperation KubegresStatefulSetSpecUpdateOperation `json:"statefulSetSpecUpdateOperation,omitempty"`
}

type KubegresStatus struct {
	LastCreatedInstanceIndex  int32                     `json:"lastCreatedInstanceIndex,omitempty"`
	BlockingOperation         KubegresBlockingOperation `json:"blockingOperation,omitempty"`
	PreviousBlockingOperation KubegresBlockingOperation `json:"previousBlockingOperation,omitempty"`
	EnforcedReplicas          int32                     `json:"enforcedReplicas,omitempty"`
	TLSTransition             TLSTransition             `json:"tlsTransition,omitempty"`
}

// TLSTransition is used to track the TLS mode transition state.
type TLSTransition struct {
	TransitionInProgress bool            `json:"transitionInProgress"`
	CurrentTransitMode   SSLMode         `json:"currentTransitMode"`
	TransitState         TLSTransitState `json:"transitState"`
	SecureSpec           TLS             `json:"secureSpec,omitempty"`
	InsecureSpec         TLS             `json:"insecureSpec,omitempty"`
}

type TLSTransitState string

const (
	TLSTransitStateNone       TLSTransitState = ""
	TLSTransitStateToSecure   TLSTransitState = "to_secure"
	TLSTransitStateToInsecure TLSTransitState = "to_insecure"
)

// ----------------------- RESOURCE ---------------------------------------

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Kubegres is the Schema for the kubegres API
type Kubegres struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubegresSpec   `json:"spec,omitempty"`
	Status KubegresStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubegresList contains a list of Kubegres
type KubegresList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kubegres `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kubegres{}, &KubegresList{})
}
