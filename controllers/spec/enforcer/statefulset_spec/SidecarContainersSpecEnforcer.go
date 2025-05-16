package statefulset_spec

import (
	"reflect"
	"strings"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
)

type SidecarContainersSpecEnforcer struct {
	kubegresContext ctx.KubegresContext
}

func (c *SidecarContainersSpecEnforcer) GetSpecName() string {
	return "ContainersSpec"
}

func (c *SidecarContainersSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {
	expectedContainers := c.kubegresContext.Kubegres.Spec.SidecarContainers
	runningSidecarContainers := statefulSet.Spec.Template.Spec.Containers[1:]

	if len(runningSidecarContainers) != len(expectedContainers) {
		return c.createDifference(runningSidecarContainers, expectedContainers)
	}

	byName := make(map[string]v1.Container)
	for _, runningContainer := range statefulSet.Spec.Template.Spec.Containers {
		byName[runningContainer.Name] = runningContainer
	}

	for _, wantSidecarContainer := range expectedContainers {
		runningContainer, found := byName[wantSidecarContainer.Name]
		if !found {
			return c.createDifference(runningSidecarContainers, expectedContainers)
		}

		// compare fields that use primitive types and fields that are not set by default
		if runningContainer.Image != wantSidecarContainer.Image ||
			runningContainer.WorkingDir != wantSidecarContainer.WorkingDir ||
			runningContainer.Stdin != wantSidecarContainer.Stdin ||
			runningContainer.StdinOnce != wantSidecarContainer.StdinOnce ||
			runningContainer.TTY != wantSidecarContainer.TTY {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}

		// compare collections fields or pointers
		if !reflect.DeepEqual(runningContainer.Command, wantSidecarContainer.Command) ||
			!reflect.DeepEqual(runningContainer.Args, wantSidecarContainer.Args) ||
			!reflect.DeepEqual(runningContainer.Ports, wantSidecarContainer.Ports) ||
			!reflect.DeepEqual(runningContainer.Env, wantSidecarContainer.Env) ||
			!reflect.DeepEqual(runningContainer.EnvFrom, wantSidecarContainer.EnvFrom) ||
			!reflect.DeepEqual(runningContainer.Resources, wantSidecarContainer.Resources) ||
			!reflect.DeepEqual(runningContainer.VolumeDevices, wantSidecarContainer.VolumeDevices) ||
			!reflect.DeepEqual(runningContainer.ReadinessProbe, wantSidecarContainer.ReadinessProbe) ||
			!reflect.DeepEqual(runningContainer.LivenessProbe, wantSidecarContainer.LivenessProbe) ||
			!reflect.DeepEqual(runningContainer.StartupProbe, wantSidecarContainer.StartupProbe) ||
			!reflect.DeepEqual(runningContainer.Lifecycle, wantSidecarContainer.Lifecycle) ||
			!reflect.DeepEqual(runningContainer.SecurityContext, wantSidecarContainer.SecurityContext) {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}

		// fields where K8s sets defaults:
		// - VolumeMounts
		// - TerminationMessagePath
		// - TerminationMessagePolicy
		// - ImagePullPolicy
		// compare them only if they are set in the spec
		if len(wantSidecarContainer.VolumeMounts) > 0 && !reflect.DeepEqual(runningContainer.VolumeMounts, wantSidecarContainer.VolumeMounts) {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}
		if wantSidecarContainer.TerminationMessagePath != "" && runningContainer.TerminationMessagePath != wantSidecarContainer.TerminationMessagePath {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}
		if wantSidecarContainer.TerminationMessagePolicy != "" && runningContainer.TerminationMessagePolicy != wantSidecarContainer.TerminationMessagePolicy {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}
		if wantSidecarContainer.ImagePullPolicy != "" && runningContainer.ImagePullPolicy != wantSidecarContainer.ImagePullPolicy {
			return c.createDifference([]v1.Container{runningContainer}, []v1.Container{wantSidecarContainer})
		}
	}
	// if we reach this point, it means that all sidecar containers are equal
	return StatefulSetSpecDifference{}
}

func (c *SidecarContainersSpecEnforcer) createDifference(runningSidecarContainers []v1.Container, expectedContainers []v1.Container) StatefulSetSpecDifference {
	var currentContainerNames, expectedContainerNames strings.Builder
	for _, container := range runningSidecarContainers {
		currentContainerNames.WriteString(container.Name + ", ")
	}
	for _, container := range expectedContainers {
		expectedContainerNames.WriteString(container.Name + ", ")
	}

	difference := StatefulSetSpecDifference{
		SpecName: c.GetSpecName(),
		Current:  currentContainerNames.String(),
		Expected: expectedContainerNames.String(),
	}
	return difference
}

func (c *SidecarContainersSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {
	expectedContainers := c.kubegresContext.Kubegres.Spec.SidecarContainers
	if len(expectedContainers) == 0 {
		// if there are no sidecar containers defined, we remove all sidecars
		statefulSet.Spec.Template.Spec.Containers = statefulSet.Spec.Template.Spec.Containers[:1]
		return true, nil
	}
	for _, container := range expectedContainers {
		statefulSet.Spec.Template.Spec.Containers = append(statefulSet.Spec.Template.Spec.Containers, container)
	}
	return true, nil
}

func (c *SidecarContainersSpecEnforcer) OnSpecEnforcedSuccessfully(statefulSet *apps.StatefulSet) error {
	c.kubegresContext.Log.InfoEvent("StatefulSetOperation", "Sidecar containers spec enforced successfully", "StatefulSet name", statefulSet.Name, "Sidecar containers", c.kubegresContext.Kubegres.Spec.SidecarContainers)
	return nil
}

func CreateContainersSpecEnforcer(ctx ctx.KubegresContext) SidecarContainersSpecEnforcer {
	return SidecarContainersSpecEnforcer{
		kubegresContext: ctx,
	}
}
