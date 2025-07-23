package statefulset_spec

import (
	"fmt"
	"strings"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/spec/template"
)

type TLSSpecEnforcer struct {
	kubegresContext     ctx.KubegresContext
	tlsConfigSpecHelper template.TLSConfigSpecHelper
}

func CreateTLSSpecEnforcer(kubegresContext ctx.KubegresContext, tlsConfigSpecHelper template.TLSConfigSpecHelper) TLSSpecEnforcer {
	return TLSSpecEnforcer{
		kubegresContext:     kubegresContext,
		tlsConfigSpecHelper: tlsConfigSpecHelper,
	}
}

func (r *TLSSpecEnforcer) GetSpecName() string {
	return "TLS"
}

func (r *TLSSpecEnforcer) isEnabled() bool {
	return r.kubegresContext.Kubegres.Spec.TLS.Enabled
}

func (r *TLSSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {

	if !r.isEnabled() {
		for _, volume := range statefulSet.Spec.Template.Spec.Volumes {
			if volume.Name == ctx.TLSVolumeName {
				return StatefulSetSpecDifference{
					SpecName: "TLS (Volume)",
					Current:  "Found TLS volume, but TLS is disabled",
					Expected: "No TLS volume should be present when TLS is disabled",
				}
			}
		}

		for _, container := range statefulSet.Spec.Template.Spec.Containers {
			for _, volumeMount := range container.VolumeMounts {
				if volumeMount.Name == ctx.TLSVolumeName {
					return StatefulSetSpecDifference{
						SpecName: "TLS (Container.VolumeMount)",
						Current:  "Found TLS volume mount, but TLS is disabled",
						Expected: "No TLS volume mount should be present when TLS is disabled",
					}
				}
			}
		}

		for _, initContainer := range statefulSet.Spec.Template.Spec.InitContainers {
			for _, volumeMount := range initContainer.VolumeMounts {
				if volumeMount.Name == ctx.TLSVolumeName {
					return StatefulSetSpecDifference{
						SpecName: "TLS (InitContainer.VolumeMount)",
						Current:  "Found TLS volume mount in init container, but TLS is disabled",
						Expected: "No TLS volume mount should be present in init containers when TLS is disabled",
					}
				}
			}
		}

		return StatefulSetSpecDifference{} // No differences found when TLS is disabled
	}

	currentTLSVolume := r.getCurrentTLSVolume(statefulSet)
	expectedTLSVolume := template.TLSVolume(r.kubegresContext.Kubegres.Spec.TLS)

	if !r.compareVolume(currentTLSVolume, expectedTLSVolume) {
		return StatefulSetSpecDifference{
			SpecName: "TLS (Volume)",
			Current:  r.volumeToString(currentTLSVolume),
			Expected: r.volumeToString(expectedTLSVolume),
		}
	}

	currentByConainter, currentByInitContainer := r.getCurrentTLSVolumeMounts(statefulSet)
	expectedTLSVolumeMount := template.TLSVolumeMount(r.kubegresContext.Kubegres.Spec.TLS)

	if !r.compareVolumeMounts(currentByConainter, currentByInitContainer, expectedTLSVolumeMount) {
		return StatefulSetSpecDifference{
			SpecName: "TLS (VolumeMount)",
			Current:  r.volumeMountsToString(currentByConainter, currentByInitContainer),
			Expected: r.volumeMountToString(expectedTLSVolumeMount),
		}
	}

	if differences, ok := r.tlsConfigSpecHelper.HaveVolumeMountsUpdatedTLSConfigMapKeys(statefulSet); !ok {
		return StatefulSetSpecDifference{
			SpecName: "TLS (ConfigMap Keys)",
			Current:  "Volume mounts do not have updated TLS config map keys",
			Expected: differences,
		}
	}

	return StatefulSetSpecDifference{}
}

func (r *TLSSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {

	if r.isEnabled() {
		r.addTLSVolume(statefulSet)
		r.addTLSVolumeMounts(statefulSet)
		r.tlsConfigSpecHelper.UpdateVolumeMountsWithTLSConfigMapKeys(statefulSet)
		return true, nil
	}

	r.removeTLSVolume(statefulSet)
	r.removeTLSVolumeMounts(statefulSet)
	r.tlsConfigSpecHelper.UndoVolumeMountsWithTLSConfigMapKeys(statefulSet)
	return true, nil

}

func (r *TLSSpecEnforcer) OnSpecEnforcedSuccessfully(*apps.StatefulSet) error {
	// No specific actions needed after enforcing TLS spec
	return nil
}

func (r *TLSSpecEnforcer) getCurrentTLSVolume(statefulSet *apps.StatefulSet) core.Volume {
	for _, volume := range statefulSet.Spec.Template.Spec.Volumes {
		if volume.Name == ctx.TLSVolumeName {
			return volume
		}
	}
	return core.Volume{}
}

func (r *TLSSpecEnforcer) getCurrentTLSVolumeMounts(statefulSet *apps.StatefulSet) (map[string]core.VolumeMount, map[string]core.VolumeMount) {
	volsByContainer := make(map[string]core.VolumeMount, len(statefulSet.Spec.Template.Spec.Containers))
	for _, container := range statefulSet.Spec.Template.Spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == ctx.TLSVolumeName {
				volsByContainer[container.Name] = volumeMount
			}
		}
	}

	volsByInitContainer := make(map[string]core.VolumeMount, len(statefulSet.Spec.Template.Spec.InitContainers))
	for _, initContainer := range statefulSet.Spec.Template.Spec.InitContainers {
		for _, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name == ctx.TLSVolumeName {
				volsByInitContainer[initContainer.Name] = volumeMount
			}
		}
	}
	return volsByContainer, volsByInitContainer
}

func (r *TLSSpecEnforcer) compareVolume(current, expected core.Volume) bool {
	return current.Name == expected.Name &&
		current.VolumeSource.Secret != nil &&
		current.VolumeSource.Secret.SecretName == expected.VolumeSource.Secret.SecretName &&
		current.VolumeSource.Secret.DefaultMode != nil &&
		*current.VolumeSource.Secret.DefaultMode == *expected.VolumeSource.Secret.DefaultMode
}

func (r *TLSSpecEnforcer) compareVolumeMounts(currentByContainer, currentByInitContainer map[string]core.VolumeMount, expected core.VolumeMount) bool {
	for _, volumeMount := range currentByContainer {
		if !r.compareVolumeMount(volumeMount, expected) {
			return false
		}
	}

	for _, volumeMount := range currentByInitContainer {
		if !r.compareVolumeMount(volumeMount, expected) {
			return false
		}
	}

	return true
}

func (r *TLSSpecEnforcer) compareVolumeMount(volumeMount core.VolumeMount, expected core.VolumeMount) bool {
	return volumeMount.Name == expected.Name &&
		volumeMount.MountPath == expected.MountPath &&
		volumeMount.SubPath == expected.SubPath &&
		volumeMount.ReadOnly == expected.ReadOnly
}

func (r *TLSSpecEnforcer) addTLSVolume(statefulSet *apps.StatefulSet) {
	tlsVolume := template.TLSVolume(r.kubegresContext.Kubegres.Spec.TLS)

	var alreadyExists bool
	for i, volume := range statefulSet.Spec.Template.Spec.Volumes {
		if volume.Name == ctx.TLSVolumeName {
			if !r.compareVolume(volume, tlsVolume) {
				// override the existing volume with the expected one
				statefulSet.Spec.Template.Spec.Volumes[i] = tlsVolume
			}
			alreadyExists = true
			break
		}
	}

	if alreadyExists {
		return
	}

	statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, tlsVolume)
}

func (r *TLSSpecEnforcer) addTLSVolumeMounts(statefulSet *apps.StatefulSet) {
	tlsVolumeMount := template.TLSVolumeMount(r.kubegresContext.Kubegres.Spec.TLS)
	for i := range statefulSet.Spec.Template.Spec.Containers {
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		var alreadyExists bool
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == ctx.TLSVolumeName {
				if !r.compareVolumeMount(volumeMount, tlsVolumeMount) {
					// override the existing volume mount with the expected one
					container.VolumeMounts = append(container.VolumeMounts, tlsVolumeMount)
				}
				alreadyExists = true
				break
			}
		}

		if !alreadyExists {
			container.VolumeMounts = append(container.VolumeMounts, tlsVolumeMount)
		}
	}
	for i := range statefulSet.Spec.Template.Spec.InitContainers {
		initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
		var alreadyExists bool
		for _, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name == ctx.TLSVolumeName {
				if !r.compareVolumeMount(volumeMount, tlsVolumeMount) {
					// override the existing volume mount with the expected one
					initContainer.VolumeMounts = append(initContainer.VolumeMounts, tlsVolumeMount)
				}
				alreadyExists = true
				break
			}
		}

		if !alreadyExists {
			initContainer.VolumeMounts = append(initContainer.VolumeMounts, tlsVolumeMount)
		}
	}
}

func (r *TLSSpecEnforcer) removeTLSVolume(statefulSet *apps.StatefulSet) {
	newVolumes := make([]core.Volume, 0, len(statefulSet.Spec.Template.Spec.Volumes)-1)
	for _, volume := range statefulSet.Spec.Template.Spec.Volumes {
		if volume.Name != ctx.TLSVolumeName {
			newVolumes = append(newVolumes, volume)
		}
	}
	statefulSet.Spec.Template.Spec.Volumes = newVolumes
}

func (r *TLSSpecEnforcer) removeTLSVolumeMounts(statefulSet *apps.StatefulSet) {
	for i := range statefulSet.Spec.Template.Spec.Containers {
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		newVolumeMounts := make([]core.VolumeMount, 0, len(container.VolumeMounts)-1)
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name != ctx.TLSVolumeName {
				newVolumeMounts = append(newVolumeMounts, volumeMount)
			}
		}
		container.VolumeMounts = newVolumeMounts
	}

	for i := range statefulSet.Spec.Template.Spec.InitContainers {
		initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
		newVolumeMounts := make([]core.VolumeMount, 0, len(initContainer.VolumeMounts)-1)
		for _, volumeMount := range initContainer.VolumeMounts {
			if volumeMount.Name != ctx.TLSVolumeName {
				newVolumeMounts = append(newVolumeMounts, volumeMount)
			}
		}
		initContainer.VolumeMounts = newVolumeMounts
	}
}

func (r *TLSSpecEnforcer) volumeToString(volume core.Volume) string {
	str := strings.Builder{}
	str.WriteString("Name: " + volume.Name)
	if volume.VolumeSource.Secret == nil {
		str.WriteString(", Secret: nil")
		return str.String()
	}
	str.WriteString(", SecretName: " + volume.VolumeSource.Secret.SecretName)
	if volume.VolumeSource.Secret.DefaultMode == nil {
		str.WriteString(", DefaultMode: nil")
		return str.String()
	}
	str.WriteString(", DefaultMode: " + string(*volume.VolumeSource.Secret.DefaultMode))
	return str.String()
}

func (r *TLSSpecEnforcer) volumeMountToString(volumeMount core.VolumeMount) string {
	return "Name: " + volumeMount.Name +
		", MountPath: " + volumeMount.MountPath +
		", ReadOnly: " + fmt.Sprintf("%v", volumeMount.ReadOnly)
}

func (r *TLSSpecEnforcer) volumeMountsToString(volsByContainer, volsByInitContainer map[string]core.VolumeMount) string {
	var result string
	for containerName, volumeMount := range volsByContainer {
		result += fmt.Sprintf("Container: %s, %s; ", containerName, r.volumeMountToString(volumeMount))
	}
	for initContainerName, volumeMount := range volsByInitContainer {
		result += fmt.Sprintf("InitContainer: %s, %s; ", initContainerName, r.volumeMountToString(volumeMount))
	}
	return result
}
