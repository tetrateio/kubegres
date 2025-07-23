package template

import (
	"fmt"
	"strings"

	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
)

type TLSConfigSpecHelper struct {
	kubegresContext ctx.KubegresContext
}

func CreateTLSConfigSpecHelper(kubegresContext ctx.KubegresContext) TLSConfigSpecHelper {
	return TLSConfigSpecHelper{kubegresContext: kubegresContext}
}

func TLSVolume(tls apiv1.TLS) corev1.Volume {
	defaultMode := ctx.DefaultTLSVolumeMode
	return corev1.Volume{
		Name: ctx.TLSVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  tls.SecretName,
				DefaultMode: &defaultMode,
			},
		},
	}
}

func TLSVolumeMount(tls apiv1.TLS) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      ctx.TLSVolumeName,
		MountPath: tls.MountPath,
		ReadOnly:  true,
	}
}

func (r *TLSConfigSpecHelper) ConfigureStatefulSet(statefulSet *apps.StatefulSet) {
	if r.kubegresContext.Kubegres.Spec.TLS.Enabled {
		r.UpdateVolumeMountsWithTLSConfigMapKeys(statefulSet)
		r.OverrideDefaultProbesWithTLS(statefulSet)
		return
	}

	r.UndoVolumeMountsWithTLSConfigMapKeys(statefulSet)
	// TODO (sergicastro) undo readiness and liveness probes
}

func (r *TLSConfigSpecHelper) UpdateVolumeMountsWithTLSConfigMapKeys(statefulSet *apps.StatefulSet) {
	for i := range statefulSet.Spec.Template.Spec.Containers {
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		for j, volumeMount := range container.VolumeMounts {
			if volumeMount.Name != ctx.BaseConfigMapVolumeName {
				continue
			}

			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.OriginalKey && replacement.DoesApplyContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					container.VolumeMounts[j].SubPath = replacement.ReplacementKey
				}
			}
		}
	}

	for i := range statefulSet.Spec.Template.Spec.InitContainers {
		initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
		for j, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name != ctx.BaseConfigMapVolumeName {
				continue
			}

			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.OriginalKey && replacement.DoesApplyInitContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					initContainer.VolumeMounts[j].SubPath = replacement.ReplacementKey
				}
			}
		}
	}
}

func (r *TLSConfigSpecHelper) UndoVolumeMountsWithTLSConfigMapKeys(statefulSet *apps.StatefulSet) {
	for i := range statefulSet.Spec.Template.Spec.Containers {
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		for j, volumeMount := range container.VolumeMounts {
			if volumeMount.Name != ctx.BaseConfigMapVolumeName {
				continue
			}

			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.ReplacementKey && replacement.DoesApplyContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					container.VolumeMounts[j].SubPath = replacement.OriginalKey
				}
			}
		}
	}

	for i := range statefulSet.Spec.Template.Spec.InitContainers {
		initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
		for j, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name != ctx.BaseConfigMapVolumeName {
				continue
			}

			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.ReplacementKey && replacement.DoesApplyInitContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					initContainer.VolumeMounts[j].SubPath = replacement.OriginalKey
				}
			}
		}
	}
}

func (r *TLSConfigSpecHelper) HaveVolumeMountsUpdatedTLSConfigMapKeys(statefulSet *apps.StatefulSet) (string, bool) {
	var containersMissUpdates, initContainersMissUpdates bool

	var noUpdated strings.Builder
	for _, container := range statefulSet.Spec.Template.Spec.Containers {
		var (
			missesUpdate       bool
			containerNoUpdated strings.Builder
		)
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == ctx.BaseConfigMapVolumeName {
				continue
			}
			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.OriginalKey && replacement.DoesApplyContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					containerNoUpdated.WriteString("- volumeMount " + volumeMount.Name +
						" with subPath " + volumeMount.SubPath +
						" should have subPath " + replacement.ReplacementKey + ",")
					missesUpdate = true
					break
				}
			}
		}
		if missesUpdate {
			noUpdated.WriteString("Container " + container.Name + " has no updated volumeMounts: " + containerNoUpdated.String() + ";;")
			containersMissUpdates = true
		}
	}

	for _, initContainer := range statefulSet.Spec.Template.Spec.InitContainers {
		var (
			missesUpdate           bool
			initContainerNoUpdated strings.Builder
		)
		for _, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name == ctx.BaseConfigMapVolumeName {
				continue
			}
			for _, replacement := range states.TLSConfigKeyReplacements {
				if volumeMount.SubPath == replacement.OriginalKey && replacement.DoesApplyInitContainer() && replacement.DoesApplyStatefulSet(statefulSet) {
					missesUpdate = true
					initContainerNoUpdated.WriteString("- volumeMount " + volumeMount.Name +
						" with subPath " + volumeMount.SubPath +
						" should have subPath " + replacement.ReplacementKey + ",")
					break
				}
			}
		}
		if missesUpdate {
			noUpdated.WriteString("InitContainer " + initContainer.Name + " has no updated volumeMounts: " + initContainerNoUpdated.String() + ";;")
			initContainersMissUpdates = true
		}
	}

	return noUpdated.String(), !containersMissUpdates && !initContainersMissUpdates
}

func (r *TLSConfigSpecHelper) OverrideDefaultReadinessProbeWithTLS(probe *corev1.Probe) {
	if r.kubegresContext.Kubegres.Spec.TLS.Enabled && r.kubegresContext.Kubegres.Spec.Probe.ReadinessProbe == nil {
		r.overrideDefaultProbeWithTLS(probe)
	}
}
func (r *TLSConfigSpecHelper) OverrideDefaultLivenessProbeWithTLS(probe *corev1.Probe) {
	if r.kubegresContext.Kubegres.Spec.TLS.Enabled && r.kubegresContext.Kubegres.Spec.Probe.LivenessProbe == nil {
		r.overrideDefaultProbeWithTLS(probe)
	}
}

func (r *TLSConfigSpecHelper) overrideDefaultProbeWithTLS(probe *corev1.Probe) {
	tls := r.kubegresContext.Kubegres.Spec.TLS
	probe.Exec.Command = tlsProbeCommand(tls)
}

func (r *TLSConfigSpecHelper) OverrideDefaultProbesWithTLS(statefulSet *apps.StatefulSet) {
	r.OverrideDefaultReadinessProbeWithTLS(statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe)
	r.OverrideDefaultLivenessProbeWithTLS(statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe)
}

func tlsProbeCommand(tls apiv1.TLS) []string {
	return []string{
		"sh",
		"-c",
		fmt.Sprintf("PGPASSWORD=$POSTGRES_PASSWORD psql \"sslmode=verify-ca "+
			"sslrootcert=%[1]s sslcert=%[2]s sslkey=%[3]s "+
			//"host=$POD_IP user=$POSTGRES_USER\" -c \"SELECT 1\"",
			// TODO (sergicastro) check about $POSTGRES_USER env var defined or not
			"host=$POD_IP user=postgres\" -c \"SELECT 1\"",
			tls.RootCertPath, tls.ClientCertPath, tls.ClientKeyPath),
	}
}
