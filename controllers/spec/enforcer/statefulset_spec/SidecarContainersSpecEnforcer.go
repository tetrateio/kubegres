package statefulset_spec

import (
	"reflect"
	"strings"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"reactive-tech.io/kubegres/controllers/ctx"
)

type SidecarContainersSpecEnforcer struct {
	kubegresContext ctx.KubegresContext
}

func CreateSidecarContainersSpecEnforcer(ctx ctx.KubegresContext) SidecarContainersSpecEnforcer {
	return SidecarContainersSpecEnforcer{
		kubegresContext: ctx,
	}
}

func (c *SidecarContainersSpecEnforcer) GetSpecName() string {
	return "SidecarContainersSpec"
}

func (c *SidecarContainersSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {
	expectedContainers := c.kubegresContext.Kubegres.Spec.SidecarContainers
	runningSidecarContainers := statefulSet.Spec.Template.Spec.Containers[1:]

	if len(runningSidecarContainers) != len(expectedContainers) {
		return c.createDifference(runningSidecarContainers, expectedContainers)
	}

	runningContainersByName := make(map[string]v1.Container)
	for _, runningContainer := range runningSidecarContainers {
		runningContainersByName[runningContainer.Name] = runningContainer
	}

	for _, wantSidecarContainer := range expectedContainers {
		name := wantSidecarContainer.Name
		runningContainer, found := runningContainersByName[name]
		if !found {
			return c.createDifference(runningSidecarContainers, expectedContainers)
		}

		// fields where K8s sets defaults:
		// - VolumeMounts
		// - TerminationMessagePath
		// - TerminationMessagePolicy
		// - ImagePullPolicy
		// copy defaults from the running container if they are not set in the spec
		if len(wantSidecarContainer.VolumeMounts) == 0 {
			wantSidecarContainer.VolumeMounts = runningContainer.VolumeMounts
		}
		if wantSidecarContainer.TerminationMessagePath == "" {
			wantSidecarContainer.TerminationMessagePath = runningContainer.TerminationMessagePath
		}
		if wantSidecarContainer.TerminationMessagePolicy == "" {
			wantSidecarContainer.TerminationMessagePolicy = runningContainer.TerminationMessagePolicy
		}
		if wantSidecarContainer.ImagePullPolicy == "" {
			wantSidecarContainer.ImagePullPolicy = runningContainer.ImagePullPolicy
		}

		if !reflect.DeepEqual(runningContainer, wantSidecarContainer) {
			return c.createDifferenceDetailed(runningContainer, wantSidecarContainer)
		}
	}

	// if we reach this point, it means that all sidecar containers are equal
	return StatefulSetSpecDifference{}
}

func (c *SidecarContainersSpecEnforcer) createDifference(currentSidecarContainers []v1.Container, expectedSidecarContainers []v1.Container) StatefulSetSpecDifference {
	currentContainers := make([]string, 0, len(currentSidecarContainers))
	for _, container := range currentSidecarContainers {
		current, _ := json.Marshal(container)
		currentContainers = append(currentContainers, string(current))
	}

	expectedContainers := make([]string, 0, len(expectedSidecarContainers))
	for _, container := range expectedSidecarContainers {
		expected, _ := json.Marshal(container)
		expectedContainers = append(expectedContainers, string(expected))
	}

	difference := StatefulSetSpecDifference{
		SpecName: c.GetSpecName(),
		Current:  strings.Join(expectedContainers, ","),
		Expected: strings.Join(currentContainers, ","),
	}
	return difference
}

func (c *SidecarContainersSpecEnforcer) createDifferenceDetailed(currentDetail, expectedDetail v1.Container) StatefulSetSpecDifference {
	current, _ := json.Marshal(currentDetail)
	expected, _ := json.Marshal(expectedDetail)
	difference := StatefulSetSpecDifference{
		SpecName: c.GetSpecName(),
		Current:  string(current),
		Expected: string(expected),
	}
	return difference
}

func (c *SidecarContainersSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {
	postgresContainer := statefulSet.Spec.Template.Spec.Containers[0] // the first container is always the postgres container
	expectedContainers := c.kubegresContext.Kubegres.Spec.SidecarContainers
	statefulSet.Spec.Template.Spec.Containers = append([]v1.Container{postgresContainer}, expectedContainers...)
	c.kubegresContext.Log.Info("--> sidecar containers spec", "len(spec.sidecarContainers)", len(expectedContainers))
	c.kubegresContext.Log.Info("--> Enforcing sidecar containers spec", "len(statefulset.spec.template.spec.containers)", len(statefulSet.Spec.Template.Spec.Containers))
	if len(statefulSet.Spec.Template.Spec.Containers) >= 2 {
		c.kubegresContext.Log.Info("--> cmd", "len(statefulSet.Spec.Template.Spec.Containers[1].Command)", len(statefulSet.Spec.Template.Spec.Containers[1].Command))
	}
	return true, nil
}

func (c *SidecarContainersSpecEnforcer) OnSpecEnforcedSuccessfully(statefulSet *apps.StatefulSet) error {
	c.kubegresContext.Log.InfoEvent("StatefulSetOperation", "Sidecar containers spec enforced successfully", "StatefulSet name", statefulSet.Name, "Sidecar containers", c.kubegresContext.Kubegres.Spec.SidecarContainers)
	return nil
}
