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

var _ = Describe("Setting Kubegres spec 'replica'", Label("group:5"), func() {

	var test = SpecReplicaTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to nil", func() {

		It("THEN a validation error event should be logged", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to nil'")

			test.givenNewKubegresSpecIsSetToNil()

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLogged()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to nil'")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to 0", func() {

		It("THEN a validation error event should be logged", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 0'")

			test.givenNewKubegresSpecIsSetTo(0)

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLogged()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 0'")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to 1", func() {

		It("THEN 1 primary and 0 replica should be created", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 1'")

			test.givenNewKubegresSpecIsSetTo(1)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 0)

			test.thenDeployedKubegresSpecShouldBeSetTo(1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 1'")
		})

	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to 2", func() {

		It("THEN 1 primary and 2 replica should be created", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 2'")

			test.givenNewKubegresSpecIsSetTo(2)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 1)

			test.thenDeployedKubegresSpecShouldBeSetTo(2)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 2'")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to 2 and 'resources' set to a value", func() {

		It("THEN replica should be created with and resources are set for both initContainer and container", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 2 and 'resources' set to a value'")

			resources := test.givenResources("2", "2Gi", "1", "1Gi")

			test.givenNewKubegresSpecWithReplicasAndResources(2, resources)
			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 1)

			test.replicaShouldHaveResourcesSet(resources)

			test.keepCreatedResourcesForNextTest = true

		})

		It("THEN existing Kubegres is updated with 'resources' the 'resources' should be updated for both initContainer and container", func() {
			log.Print("START OF: Test 'THEN existing Kubegres is updated with 'resources' the 'resources' should be updated for both initContainer and container'")

			resources := test.givenResources("4", "4Gi", "2", "2Gi")
			test.givenExistingKubegresSpecResourcesIsSetTo(resources)

			test.whenKubernetesIsUpdated()

			test.thenPodsStatesShouldBe(1, 1)

			test.replicaShouldHaveResourcesSet(resources)

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN existing Kubegres is updated with 'resources' the 'resources' should be updated for both initContainer and container'")
		})

	})

	Context("GIVEN new Kubegres is created with spec 'replica' set to 3 and then it is updated to different values", func() {

		It("GIVEN new Kubegres is created with spec 'replica' set to 3 THEN 1 primary and 2 replica should be created", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 3'")

			test.givenNewKubegresSpecIsSetTo(3)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(3)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 3'")
		})

		It("GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4 THEN 1 more replica should be created", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4'")

			test.givenExistingKubegresSpecIsSetTo(4)

			test.whenKubernetesIsUpdated()

			test.thenPodsStatesShouldBe(1, 3)

			test.thenDeployedKubegresSpecShouldBeSetTo(4)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4'")
		})

		It("GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3 THEN 1 replica should be deleted", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3'")

			test.givenExistingKubegresSpecIsSetTo(3)

			test.whenKubernetesIsUpdated()

			test.thenPodsStatesShouldBe(1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(3)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3'")
		})

		It("GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1 THEN 2 replica should be deleted", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1'")

			test.givenExistingKubegresSpecIsSetTo(1)

			test.whenKubernetesIsUpdated()

			test.thenPodsStatesShouldBe(1, 0)

			test.thenDeployedKubegresSpecShouldBeSetTo(1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1'")
		})
	})

})

type SpecReplicaTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *SpecReplicaTest) givenNewKubegresSpecIsSetToNil() {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Replicas = nil
}

func (r *SpecReplicaTest) givenNewKubegresSpecIsSetTo(specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecReplicaTest) givenNewKubegresSpecWithReplicasAndResources(specNbreReplicas int32, resources corev1.ResourceRequirements) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
	r.kubegresResource.Spec.Resources = resources
}

func (r *SpecReplicaTest) givenResources(cpuLimit, memLimit, cpuReq, memReq string) corev1.ResourceRequirements {
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

func (r *SpecReplicaTest) givenExistingKubegresSpecIsSetTo(specNbreReplicas int32) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecReplicaTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *SpecReplicaTest) whenKubernetesIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}

func (r *SpecReplicaTest) thenErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message:   "In the Resources Spec the value of 'spec.replicas' is undefined. Please set a value otherwise this operator cannot work correctly.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecReplicaTest) thenPodsStatesShouldBe(nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		pods, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres pods")
			return false
		}

		if pods.AreAllReady &&
			pods.NbreDeployedPrimary == nbrePrimary &&
			pods.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecReplicaTest) thenDeployedKubegresSpecShouldBeSetTo(specNbreReplicas int32) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	Expect(*r.kubegresResource.Spec.Replicas).Should(Equal(specNbreReplicas))
}

func (r *SpecReplicaTest) replicaShouldHaveResourcesSet(resources corev1.ResourceRequirements) {

	Eventually(func() bool {
		pods, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres pods")
			return false
		}

		for _, kubegresResource := range pods.Resources {
			for _, initContainer := range kubegresResource.StatefulSet.Spec.Template.Spec.InitContainers {
				if !reflect.DeepEqual(initContainer.Resources, resources) {
					log.Printf("InitContainer resources are not equal. got: %v want: %v", initContainer.Resources, resources)
					return false
				}
			}
			for _, container := range kubegresResource.StatefulSet.Spec.Template.Spec.Containers {
				if !reflect.DeepEqual(container.Resources, resources) {
					log.Printf("Container resources are not equal. got: %v want: %v", container.Resources, resources)
					return false
				}
			}
		}
		return true

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecReplicaTest) givenExistingKubegresSpecResourcesIsSetTo(resources corev1.ResourceRequirements) {

	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Resources = resources
}
