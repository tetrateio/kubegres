package test

import (
	"log"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	//v12 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Setting Kubegres spec 'replica'", Label("group:5"), Label("standby"), func() {

	var test = StandByTest{}

	BeforeEach(func() {
		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCasesWithConnections(
			util.InitExternalDbConnectionDbUtil(test.resourceCreator, resourceConfigs.ServiceToSqlQueryExternalDbNodePort),
			util.InitDbConnectionDbUtil(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryReplicaDbNodePort, false),
		)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty", func() {

		It("THEN a validation error event should be logged", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty")

			test.givenNewKubegresSpecIsStandbySetToTrue()

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLogged()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to external postgres endpoint", func() {

		It("THEN replica set to 1 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set to 1 should be running and replicating data from external postgres")

			test.givenNewExternalPostgresIsCreatedAndReady()

			test.givenNewKubegresSpecIsStandbySetToTrueAndPrimaryEndpointSetToExternalPostgres()

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(0, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'replica set to 1 should be running and replicating data from external postgres")
		})

		It("THEN replica set to 2 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set to 2 should be running and replicating data from external postgres")

			test.givenExistingKubegresSpecIsSetTo(2)

			test.whenKubegresIsUpdated()

			test.thenPodsStatesShouldBe(0, 2)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN replica set to 2 should be running and replicating data from external postgres")
		})

		It("THEN replica set from 2 to 1 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set from 2 to 1 should be running and replicating data from external postgres")

			test.givenExistingKubegresSpecIsSetTo(1)

			test.whenKubegresIsUpdated()

			test.thenPodsStatesShouldBe(0, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN replica set from 2 to 1 should be running and replicating data from external postgres")
		})

	})
	//
	//	Context("GIVEN new Kubegres is created with spec 'replica' set to 0", func() {
	//
	//		It("THEN a validation error event should be logged", func() {
	//
	//			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 0'")
	//
	//			test.givenNewKubegresSpecIsSetTo(0)
	//
	//			test.whenKubegresIsCreated()
	//
	//			test.thenErrorEventShouldBeLogged()
	//
	//			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 0'")
	//		})
	//	})
	//
	//	Context("GIVEN new Kubegres is created with spec 'replica' set to 1", func() {
	//
	//		It("THEN 1 primary and 0 replica should be created", func() {
	//
	//			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 1'")
	//
	//			test.givenNewKubegresSpecIsSetTo(1)
	//
	//			test.whenKubegresIsCreated()
	//
	//			test.thenPodsStatesShouldBe(1, 0)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(1)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//
	//			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 1'")
	//		})
	//
	//	})
	//
	//	Context("GIVEN new Kubegres is created with spec 'replica' set to 2", func() {
	//
	//		It("THEN 1 primary and 2 replica should be created", func() {
	//
	//			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 2'")
	//
	//			test.givenNewKubegresSpecIsSetTo(2)
	//
	//			test.whenKubegresIsCreated()
	//
	//			test.thenPodsStatesShouldBe(1, 1)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(2)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()
	//
	//			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 2'")
	//		})
	//
	//	})
	//
	//	Context("GIVEN new Kubegres is created with spec 'replica' set to 3 and then it is updated to different values", func() {
	//
	//		It("GIVEN new Kubegres is created with spec 'replica' set to 3 THEN 1 primary and 2 replica should be created", func() {
	//
	//			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 3'")
	//
	//			test.givenNewKubegresSpecIsSetTo(3)
	//
	//			test.whenKubegresIsCreated()
	//
	//			test.thenPodsStatesShouldBe(1, 2)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(3)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()
	//
	//			test.keepCreatedResourcesForNextTest = true
	//
	//			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'replica' set to 3'")
	//		})
	//
	//		It("GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4 THEN 1 more replica should be created", func() {
	//
	//			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4'")
	//
	//			test.givenExistingKubegresSpecIsSetTo(4)
	//
	//			test.whenKubernetesIsUpdated()
	//
	//			test.thenPodsStatesShouldBe(1, 3)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(4)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()
	//
	//			test.keepCreatedResourcesForNextTest = true
	//
	//			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 4'")
	//		})
	//
	//		It("GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3 THEN 1 replica should be deleted", func() {
	//
	//			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3'")
	//
	//			test.givenExistingKubegresSpecIsSetTo(3)
	//
	//			test.whenKubernetesIsUpdated()
	//
	//			test.thenPodsStatesShouldBe(1, 2)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(3)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()
	//
	//			test.keepCreatedResourcesForNextTest = true
	//
	//			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 4 to 3'")
	//		})
	//
	//		It("GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1 THEN 2 replica should be deleted", func() {
	//
	//			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1'")
	//
	//			test.givenExistingKubegresSpecIsSetTo(1)
	//
	//			test.whenKubernetesIsUpdated()
	//
	//			test.thenPodsStatesShouldBe(1, 0)
	//
	//			test.thenDeployedKubegresSpecShouldBeSetTo(1)
	//
	//			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
	//
	//			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'replica' set from 3 to 1'")
	//		})
	//	})

})

type StandByTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *StandByTest) givenNewKubegresSpecIsStandbySetToTrue() {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Standby.Enabled = true
}

func (r *StandByTest) givenNewKubegresSpecIsStandbySetToTrueAndPrimaryEndpointSetToExternalPostgres() {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Standby.Enabled = true
	r.kubegresResource.Spec.Standby.PrimaryEndpoint = "external-postgres"
	replicas := int32(1)
	r.kubegresResource.Spec.Replicas = &replicas
}

func (r *StandByTest) givenNewExternalPostgresIsCreatedAndReady() {
	r.resourceCreator.CreateExternalPostgres()

	for {
		select {
		case <-time.After(time.Second):
			statefulSet, err := r.resourceRetriever.GetStatefulSet(resourceConfigs.StatefulSetExternalDbResourceName)
			if err != nil {
				log.Println("Error while getting StatefulSet resource : ", err)
				continue
			}
			if statefulSet.Status.AvailableReplicas == 1 {
				return
			}
		case <-time.After(resourceConfigs.TestTimeout):
			log.Println("Timeout while waiting for StatefulSet to be ready")
			return
		}
	}
}

//	func (r *SpecReplicaTest) givenNewKubegresSpecIsSetTo(specNbreReplicas int32) {
//		r.kubegresResource = resourceConfigs.LoadKubegresYaml()
//		r.kubegresResource.Spec.Replicas = &specNbreReplicas
//	}
func (r *StandByTest) givenExistingKubegresSpecIsSetTo(specNbreReplicas int32) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *StandByTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *StandByTest) whenKubegresIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}
func (r *StandByTest) thenErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: v12.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message:   "In the Resources Spec the value of 'spec.standby.primaryEndpoint' is undefined. Please set a value otherwise this operator cannot work correctly.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *StandByTest) thenPodsStatesShouldBe(nbrePrimary, nbreReplicas int) bool {
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

//func (r *SpecReplicaTest) thenDeployedKubegresSpecShouldBeSetTo(specNbreReplicas int32) {
//	var err error
//	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
//
//	if err != nil {
//		log.Println("Error while getting Kubegres resource : ", err)
//		Expect(err).Should(Succeed())
//		return
//	}
//
//	Expect(*r.kubegresResource.Spec.Replicas).Should(Equal(specNbreReplicas))
//}
