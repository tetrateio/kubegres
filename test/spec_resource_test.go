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

package test

import (
	"log"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Setting Kubegres spec 'resource'", Label("group:5"), func() {

	var test = SpecResourceTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName, k8sClientTest)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created without spec 'resources' and with spec 'replica' set to 3", func() {

		It("THEN 1 primary and 2 replica should be created without 'resources' values ", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created without spec 'resources' and with spec 'replica' set to 3'")

			test.givenNewKubegresSpecIsWithoutResources(3)

			test.whenKubegresIsCreated()

			test.thenStatefulSetStatesShouldBeWithoutResources(1, 2)

			test.thenDeployedKubegresSpecShouldWithoutResource()

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN new Kubegres is created without spec 'resources' and with spec 'replica' set to 3'")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'resources' set to a value and spec 'replica' set to 3 and later 'resources' is updated to a new value", func() {

		It("GIVEN new Kubegres is created with spec 'resources' set to a value and spec 'replica' set to 3 THEN 1 primary and 2 replica should be created with spec 'resources' set the value", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'resources' set to a value and spec 'replica' set to 3")

			resources := test.givenResources("2", "2Gi", "1", "1Gi")

			test.givenNewKubegresSpecIsSetTo(resources, 3)

			test.whenKubegresIsCreated()

			test.thenStatefulSetStatesShouldBe(resources, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(resources)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'resources' set to a value and spec 'replica' set to 3'")
		})

		It("GIVEN existing Kubegres is updated with spec 'resources' set to a new value THEN 1 primary and 2 replica should be re-deployed with spec 'resources' set the new value", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'resources' set to a new value")

			newResources := test.givenResources("2", "4Gi", "500m", "512Mi")

			test.givenExistingKubegresSpecIsSetTo(newResources)

			test.whenKubernetesIsUpdated()

			test.thenStatefulSetStatesShouldBe(newResources, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(newResources)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'resources' set to a new value")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'sidecarContainer'", func() {

		It("THEN created StatefulSets should have sidecarContainer set in pod template", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'sidecarContainer")

			containers := test.givenSidecarContainers("sidecarcontainer", "busybox")

			test.givenNewKubegresSpecHasSidecarContainersSetTo(containers)

			test.whenKubegresIsCreated()

			test.thenStatefulSetStatesShouldHaveContainer("sidecarcontainer", "busybox", nil, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'sidecarContainer")
		})

		It("THEN delete `sidecarContainer` field shouold remove the container form pod template spec", func() {
			log.Print("START OF: Test 'THEN delete `sidecarContainer` field shouold remove the container form pod template spec")

			test.givenExistingKubegresSpecSidecarContainersIsSetTo(nil)

			test.whenKubernetesIsUpdated()

			test.thenStatefulSetStatesShouldNOTHaveContainer("sidecarcontainer", "busybox", nil, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(1)
			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN delete `sidecarContainer` field shouold remove the container form pod template spec")
		})

		It("THEN add back `sidecarContainer` field should add the container back to pod template spec", func() {
			log.Print("START OF: Test 'THEN add back `sidecarContainer` field should add the container back to pod template spec")

			containers := test.givenSidecarContainers("sidecarcontainer", "busybox")
			test.givenExistingKubegresSpecSidecarContainersIsSetTo(containers)

			test.whenKubernetesIsUpdated()
			test.thenStatefulSetStatesShouldHaveContainer("sidecarcontainer", "busybox", nil, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.setAnnotationsOnExisitgKubegres(map[string]string{"foo": "bar"}) // to trigger reconciliation
			test.whenKubernetesIsUpdated()
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN add back `sidecarContainer` field should add the container back to pod template spec")
		})

		It("THEN modify `sidecarContainer` args and env should update the container in pod template spec", func() {
			log.Print("START OF: Test 'THEN modify `sidecarContainer` args and env should update the container in pod template spec")

			containers := test.givenSidecarContainers("sidecarcontainer", "busybox")
			containers[0].Command = []string{"/bin/sleep", "12345"}
			containers[0].Env = []corev1.EnvVar{{Name: "FOO", Value: "BAR"}}
			test.givenExistingKubegresSpecSidecarContainersIsSetTo(containers)

			test.whenKubernetesIsUpdated()
			test.thenStatefulSetStatesShouldHaveContainer(
				"sidecarcontainer",
				"busybox",
				[]string{"/bin/sleep", "12345"},
				[]corev1.EnvVar{{Name: "FOO", Value: "BAR"}},
			)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN modify `sidecarContainer` args and env should update the container in pod template spec")
		})

	})

})

type SpecResourceTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *SpecResourceTest) whenKubernetesIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}

func (r *SpecResourceTest) givenResources(cpuLimit, memLimit, cpuReq, memReq string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse(cpuLimit),
			"memory": resource.MustParse(memLimit),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse(cpuReq),
			"memory": resource.MustParse(memReq),
		},
	}
}

func (r *SpecResourceTest) givenNewKubegresSpecIsWithoutResources(specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Resources = corev1.ResourceRequirements{}
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecResourceTest) givenNewKubegresSpecIsSetTo(resources corev1.ResourceRequirements, specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Resources = resources
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecResourceTest) givenExistingKubegresSpecIsSetTo(resources corev1.ResourceRequirements) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Resources = resources
}

func (r *SpecResourceTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *SpecResourceTest) thenStatefulSetStatesShouldBeWithoutResources(nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		for _, resource := range kubegresResources.Resources {
			currentResources := resource.StatefulSet.Spec.Template.Spec.Containers[0].Resources
			emptyResources := corev1.ResourceRequirements{}

			if !reflect.DeepEqual(currentResources, emptyResources) {
				log.Println("StatefulSet '" + resource.StatefulSet.Name + emptyResources.String() + "  ' doesn't have the expected spec 'resources' which should be the default one. " +
					"Current value: '" + currentResources.String() + "'. Waiting...")
				return false
			}
		}

		if kubegresResources.AreAllReady &&
			kubegresResources.NbreDeployedPrimary == nbrePrimary &&
			kubegresResources.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecResourceTest) thenStatefulSetStatesShouldBe(expectedResources corev1.ResourceRequirements, nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		for _, resource := range kubegresResources.Resources {
			for i := range resource.StatefulSet.Spec.Template.Spec.Containers {
				currentResources := resource.StatefulSet.Spec.Template.Spec.Containers[i].Resources
				if !reflect.DeepEqual(currentResources, expectedResources) {
					log.Println("StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected spec 'resources': " + expectedResources.String() + " " +
						"Current value: '" + currentResources.String() + "'. Waiting...")
					return false
				}
			}
			for i := range resource.StatefulSet.Spec.Template.Spec.InitContainers {
				currentResources := resource.StatefulSet.Spec.Template.Spec.InitContainers[i].Resources
				if !reflect.DeepEqual(currentResources, expectedResources) {
					log.Println("StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected spec 'resources': " + expectedResources.String() + " " +
						"Current value: '" + currentResources.String() + "'. Waiting...")
					return false
				}
			}
		}

		if kubegresResources.AreAllReady &&
			kubegresResources.NbreDeployedPrimary == nbrePrimary &&
			kubegresResources.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecResourceTest) thenDeployedKubegresSpecShouldWithoutResource() {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}
	currentResources := r.kubegresResource.Spec.Resources
	emptyResources := corev1.ResourceRequirements{}
	Expect(currentResources).Should(Equal(emptyResources))
}

func (r *SpecResourceTest) thenDeployedKubegresSpecShouldBeSetTo(expectedResources corev1.ResourceRequirements) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	currentResources := r.kubegresResource.Spec.Resources
	Expect(currentResources).Should(Equal(expectedResources))
}

func (r *SpecResourceTest) givenSidecarContainers(name, image string) []corev1.Container {
	return []corev1.Container{
		{
			Name:    name,
			Image:   image,
			Command: []string{"/bin/sleep", "99999"},
		},
	}
}

func (r *SpecResourceTest) givenNewKubegresSpecHasSidecarContainersSetTo(containers []corev1.Container) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.SidecarContainers = containers
	r.kubegresResource.Spec.Replicas = func(i int32) *int32 { return &i }(1)
}

func (r *SpecResourceTest) thenStatefulSetStatesShouldHaveContainer(containerName string, containerImage string, cmd []string, vars []corev1.EnvVar) bool {
	return r.assertStatefulSetsResourcesContainers(containerName, containerImage, cmd, vars, true)
}

func (r *SpecResourceTest) givenExistingKubegresSpecSidecarContainersIsSetTo(containers []corev1.Container) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.SidecarContainers = containers
}

func (r *SpecResourceTest) thenStatefulSetStatesShouldHaveNbreContainers(numberOfContainers int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		if kubegresResources.AreAllReady != true {
			return false
		}

		for _, res := range kubegresResources.Resources {
			if len(res.StatefulSet.Spec.Template.Spec.Containers) != numberOfContainers {
				log.Println("StatefulSet '" + res.StatefulSet.Name + "' doesn't have the expected number of containers'")
				return false
			}
		}

		return true
	}, 100*time.Second, time.Second).Should(BeTrue())
}

func (r *SpecResourceTest) assertStatefulSetsResourcesContainers(name, image string, cmd []string, vars []corev1.EnvVar, isFound bool) bool {
	return Eventually(func() bool {
		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		if !kubegresResources.AreAllReady {
			return false
		}

		containerFound := false
		for _, resource := range kubegresResources.Resources {
			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				if container.Name == name && container.Image == image {
					if (cmd == nil && vars == nil) ||
						(cmd != nil && vars != nil && reflect.DeepEqual(container.Command, cmd) && reflect.DeepEqual(container.Env, vars)) {
						containerFound = true
						break
					}
				}
			}
		}
		log.Printf("Container found: %v, expected isFound: %v, name: %s, image: %s\n", containerFound, isFound, name, image)
		return containerFound == isFound
	}, 100*time.Second, time.Second).Should(BeTrue())
}

func (r *SpecResourceTest) thenStatefulSetStatesShouldNOTHaveContainer(name, image string, args []string, vars []corev1.EnvVar) bool {
	return r.assertStatefulSetsResourcesContainers(name, image, args, vars, false)
}

func (r *SpecResourceTest) setAnnotationsOnExisitgKubegres(annnotations map[string]string) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}
	r.kubegresResource.SetAnnotations(annnotations)
}
