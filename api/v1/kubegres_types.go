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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ----------------------- SPEC -------------------------------------------

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

	// +optional
	// IntelligentFailover picks the Replica to promote by its PostgreSQL replication state
	// instead of Kubernetes readiness alone. Off by default.
	IntelligentFailover *IntelligentFailoverConfig `json:"intelligentFailover,omitempty"`

	// +kubebuilder:validation:Type:=string
	// +kubebuilder:validation:Pattern:="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +nullable
	// +optional
	// PrimaryStabilityWindow is how long the Primary must stay unhealthy before an automatic
	// failover starts, written as 10s, 1m, etc. It filters out brief readiness blips - a tight
	// probe, a quick restart, short resource pressure - that the Primary would recover from on
	// its own, so the cluster does not risk losing data to a failover it did not need.
	//
	// This applies on top of the readiness probe's own failureThreshold, not instead of it. When
	// unset or zero, a failover starts as soon as the Primary is seen not-ready, which is how
	// Kubegres behaved before this field existed.
	//
	// A manual failover through 'failover.promotePod' is never delayed.
	PrimaryStabilityWindow *metav1.Duration `json:"primaryStabilityWindow,omitempty"`

	// +kubebuilder:validation:Minimum:=0
	// +optional
	// MinHealthyReplicas is how many Replicas must be ready before a failover counts as
	// complete. A promoted Primary with nothing behind it has no failover target left, so a
	// second failure needs manual work. Setting this holds the failover open, which also keeps
	// the other enforcers blocked, until the cluster has rebuilt that redundancy.
	//
	// When unset or zero, a failover completes as soon as the new Primary is ready, which is how
	// Kubegres behaved before this field existed. Whatever it is set to, Kubegres emits a
	// 'FailOverReducedDurability' warning event when a failover completes with no ready Replica.
	//
	// The failover still times out after 300 seconds. If the Replicas cannot be rebuilt in that
	// time, the cluster reports a failover time-out.
	MinHealthyReplicas *int32 `json:"minHealthyReplicas,omitempty"`
}

// IntelligentFailoverConfig configures WAL-aware Replica selection.
//
// Kubernetes readiness means a Pod accepts connections. It says nothing about whether that Pod's
// replication stream is intact, or how far behind the failed Primary it is. When enabled,
// Kubegres asks each candidate Replica where it actually is and promotes the one holding the
// most of the failed Primary's history.
type IntelligentFailoverConfig struct {
	// Enabled turns on selection by replication state. Default: false, i.e. select on readiness.
	Enabled bool `json:"enabled"`

	// +optional
	// MaxReplicationLag is how far behind, in bytes, the promoted Replica may be. A Replica
	// further behind than this is not promoted; Kubegres reports that manual work is needed
	// rather than quietly throwing away that much committed data.
	//
	// Lag is measured against the last WAL position Kubegres saw on the Primary while it was
	// healthy. If there is none - for example because the operator restarted since - lag is
	// measured against the furthest-ahead candidate instead. That limits how far apart the
	// Replicas are, but not how far behind the failed Primary they all are.
	//
	// Default: 16Mi. Set to 0 to promote the best candidate however far behind it is.
	MaxReplicationLag *resource.Quantity `json:"maxReplicationLag,omitempty"`

	// +kubebuilder:validation:Type:=string
	// +kubebuilder:validation:Pattern:="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +nullable
	// +optional
	// HealthCheckTimeout is how long Kubegres waits for a Replica to report its replication
	// state. Candidates are queried at the same time, so this is roughly how much time the
	// checks add to a failover. Default: 5s.
	HealthCheckTimeout *metav1.Duration `json:"healthCheckTimeout,omitempty"`

	// +optional
	// FallbackToLegacy says what to do when Kubegres cannot read the replication state of any
	// candidate, usually a network partition between the operator and the Replicas.
	//
	// True prefers availability: fall back to selection on readiness, so the cluster recovers,
	// at the risk of promoting a Replica that is behind. False prefers durability: refuse to
	// promote and report that manual work is needed. Default: true.
	FallbackToLegacy *bool `json:"fallbackToLegacy,omitempty"`

	// +optional
	// RequireStreamingReplica requires the promoted Replica to have a live WAL stream from the
	// Primary when it is checked. This rules out Replicas whose stream has broken - for example
	// after "requested WAL segment has already been removed" - which stay Kubernetes-ready while
	// stuck at whatever they last replayed.
	//
	// It is off by default: once the Primary is gone, every surviving Replica has lost its
	// stream, so requiring one would block every failover. Turn it on when Replicas are expected
	// to keep streaming from a Primary that is unhealthy but still alive.
	RequireStreamingReplica bool `json:"requireStreamingReplica,omitempty"`

	// +optional
	// AllowUnsafeManualPromotion lets a 'failover.promotePod' request go ahead even when the
	// named Pod fails the same health checks as automatic selection. Default: false, i.e.
	// manually promoting an unhealthy or lagging Replica is refused and reported.
	AllowUnsafeManualPromotion bool `json:"allowUnsafeManualPromotion,omitempty"`
}

type KubegresScheduler struct {
	Affinity    *v1.Affinity    `json:"affinity,omitempty"`
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
}

type VolumeClaimTemplate struct {
	Name string                       `json:"name,omitempty"`
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

type PrimaryVolume struct {
	VolumeMounts         []v1.VolumeMount      `json:"volumeMounts,omitempty"`
	Volumes              []v1.Volume           `json:"volumes,omitempty"`
	VolumeClaimTemplates []VolumeClaimTemplate `json:"volumeClaimTemplates,omitempty"`
}

type Volume struct {
	VolumeMounts         []v1.VolumeMount      `json:"volumeMounts,omitempty"`
	Volumes              []v1.Volume           `json:"volumes,omitempty"`
	VolumeClaimTemplates []VolumeClaimTemplate `json:"volumeClaimTemplates,omitempty"`
	// Primary defines volume settings specific only to the primary database instance.
	// These will be appended to the general volume settings defined above.
	Primary PrimaryVolume `json:"primary,omitempty"`
}

type Probe struct {
	LivenessProbe  *v1.Probe `json:"livenessProbe,omitempty"`
	ReadinessProbe *v1.Probe `json:"readinessProbe,omitempty"`
}

type ReplicationSlots struct {
	// Enabled indicates whether the replication slots feature is activated.
	Enabled bool `json:"enabled,omitempty"`
	// DisableCleanup, when set to true, prevents automatic cleanup of inactive replication slots.
	// This can be useful in scenarios where you want to retain replication slots for manual management or specific use cases.
	// By default, this is set to false, allowing the system to clean up inactive slots based on the defined grace period.
	DisableCleanup bool `json:"disableCleanup,omitempty"`
	// +kubebuilder:validation:Optional
	// MaxWalKeepSize defines the maximum size of WAL files to retain for replication slots.
	// This helps manage disk space usage by limiting the amount of WAL data kept for replication.
	// The value should be specified in a format like "1Gi", "500Mi", etc.
	// If not set, the limit is not enforced.
	MaxWalKeepSize resource.Quantity `json:"maxWalKeepSize,omitempty"`
	// +kubebuilder:validation:Type:=string
	// +kubebuilder:validation:Pattern:="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default:="10m"
	// +nullable
	// InactiveSlotGracePeriod defines how long a replication slot can remain inactive before it becomes eligible for cleanup.
	// The value should be specified in the format: 30s, 1m, 5m, etc. Default is 10m.
	InactiveSlotGracePeriod *metav1.Duration `json:"inactiveSlotGracePeriod,omitempty"`
	// +kubebuilder:validation:Type:=string
	// +kubebuilder:validation:Pattern:="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default:="30s"
	// HealthCheckInterval defines how often the health of replication slots is checked.
	// The value should be specified in the format: 30s, 1m, 5m, etc. Default is 30s.
	HealthCheckInterval metav1.Duration `json:"healthCheckInterval,omitempty"`
}

type TLS struct {
	// SecretName is the name of the Kubernetes secret that contains the TLS certificates.
	SecretName string `json:"secretName,omitempty"`
	// RootCertPath is the path to the root certificate file.
	RootCertPath string `json:"rootCert,omitempty"`
	// ServerCertPath is the path to the server certificate file.
	ServerCertPath string `json:"serverCert,omitempty"`
	// ServerKeyPath is the path to the server key file.
	ServerKeyPath string `json:"serverKey,omitempty"`
	// ClientCertPath is the path to the client certificate file.
	ClientCertPath string `json:"clientCert,omitempty"`
	// ClientKeyPath is the path to the client key file.
	ClientKeyPath string `json:"clientKey,omitempty"`
	// SSLMode honors https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION.
	SSLMode string `json:"mode,omitempty"`
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
	ReplicationSlots   ReplicationSlots          `json:"replicationSlots,omitempty"`
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
	FailOver                  KubegresFailOverStatus    `json:"failOver,omitempty"`
}

// KubegresFailOverStatus records what the operator knows about failover readiness and the most
// recent promotion. It is saved because the stability window and the lag reference point both
// have to survive an operator restart to be any use.
type KubegresFailOverStatus struct {
	// PrimaryUnhealthySinceEpochInSeconds is when the Primary was first seen not-ready this time
	// round, and 0 while it is healthy. 'failover.primaryStabilityWindow' is measured from it.
	PrimaryUnhealthySinceEpochInSeconds int64 `json:"primaryUnhealthySinceEpochInSeconds,omitempty"`

	// LastKnownPrimaryWalLsn is the last WAL position seen on a healthy Primary, in PostgreSQL's
	// "X/Y" form. Replica lag is measured against it once the Primary is gone.
	LastKnownPrimaryWalLsn string `json:"lastKnownPrimaryWalLsn,omitempty"`

	// LastKnownPrimaryWalLsnEpochInSeconds is when LastKnownPrimaryWalLsn was seen.
	LastKnownPrimaryWalLsnEpochInSeconds int64 `json:"lastKnownPrimaryWalLsnEpochInSeconds,omitempty"`

	// LastPromotedPod is the name of the Pod promoted by the most recent failover.
	LastPromotedPod string `json:"lastPromotedPod,omitempty"`

	// LastPromotionReason explains why that Pod was chosen, e.g. "highest_lsn" or "fallback".
	LastPromotionReason string `json:"lastPromotionReason,omitempty"`

	// LastPromotionEpochInSeconds is when that promotion was started.
	LastPromotionEpochInSeconds int64 `json:"lastPromotionEpochInSeconds,omitempty"`

	// BlockedReason is set when Kubegres refused to promote any candidate and the cluster needs
	// manual work. It is cleared once a promotion succeeds.
	BlockedReason string `json:"blockedReason,omitempty"`
}

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
