package test

import (
	"log"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubegresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers"
	"reactive-tech.io/kubegres/internal/replicationslot"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

type replicationSlotsCleanupTest struct {
	resourceRetriever               util.TestResourceRetriever
	resourceCreator                 util.TestResourceCreator
	kubegresResource                *kubegresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	keepCreatedResourcesForNextTest bool
}

func (t *replicationSlotsCleanupTest) givenNewKubegresWithReplicationSlotsSettings(rs kubegresv1.ReplicationSlots, replicas int32) {
	t.kubegresResource = resourceConfigs.LoadKubegresYaml()
	t.kubegresResource.Spec.ReplicationSlots = rs
	t.kubegresResource.Spec.Replicas = &replicas
}

func (t *replicationSlotsCleanupTest) whenKubegresIsCreated() {
	t.resourceCreator.CreateKubegres(t.kubegresResource)
}

func (t *replicationSlotsCleanupTest) whenStandbyKubegresIsDeployed() {
	standbyKubegres := resourceConfigs.LoadKubegresYaml()
	standbyKubegres.Labels["role"] = "standby"
}

func (t *replicationSlotsCleanupTest) givenExistingKubegresIsUpdatedWithReplicationSlotsSettings(slots kubegresv1.ReplicationSlots) {
	kubegres, err := t.resourceRetriever.GetKubegres()
	Expect(err).ToNot(HaveOccurred(), "Failed to retrieve existing Kubegres resources")
	kubegres.Spec.ReplicationSlots = slots
	t.kubegresResource = kubegres
}

func (t *replicationSlotsCleanupTest) whenKubegresIsUpdated() {
	t.resourceCreator.UpdateResource(t.kubegresResource, "Kubegres")
}

func (t *replicationSlotsCleanupTest) thenEventShouldNotBeRecorded(event util.EventRecord) {
	Expect(eventRecorderTest.CheckEventExist(event)).To(BeFalse())
}

func (t *replicationSlotsCleanupTest) thenEventShouldHaveOccurred(event util.EventRecord, timeout, interval time.Duration) {
	Eventually(func() bool {
		eventExist := eventRecorderTest.CheckEventExist(event)
		if !eventExist {
			log.Println("event not found: ", event)
			return false
		}
		return true
	}, timeout, interval).Should(BeTrue())
}

var testSlotRemovedEvent = util.EventRecord{
	Eventtype: v1.EventTypeNormal,
	Reason:    controllers.ReplicationSlotDeletedReason,
	Message:   "successfully deleted replication slot test_slot",
}

var _ = Describe("Setting Kubegres replication slots cleanup settings", func() {

	var test replicationSlotsCleanupTest

	BeforeEach(func() {
		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName, k8sClientTest)
		eventRecorderTest.RemoveAllEvents()
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	It("should not cleanup inactive replication slots when cleanup is disabled", func() {
		disableGracePeriod := time.Duration(0)
		healthCheckInterval := time.Second
		slots := kubegresv1.ReplicationSlots{
			Enabled:                 true,
			DisableCleanup:          true,
			MaxWalKeepSize:          resource.MustParse("10Mi"),
			InactiveSlotGracePeriod: &disableGracePeriod,
			HealthCheckInterval:     healthCheckInterval,
		}

		test.givenNewKubegresWithReplicationSlotsSettings(slots, 2)

		test.whenKubegresIsCreated()

		test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
		test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

		Expect(test.dbQueryTestCases.CreateReplicationSlot("test_slot")).To(Succeed())

		time.Sleep(2 * healthCheckInterval)

		test.thenEventShouldNotBeRecorded(testSlotRemovedEvent)

		replicationSlots := test.dbQueryTestCases.GetReplicationSlots()
		Expect(replicationSlots).To(HaveLen(2))

		Expect(replicationSlots).To(ContainElement(replicationslot.ReplicationSlot{
			Name:   "test_slot",
			Active: false,
		}))

		test.keepCreatedResourcesForNextTest = true
	})

	It("should cleanup inactive replication slots when cleanup is enabled", func() {

		disableGracePeriod := time.Duration(0)
		healthCheckInterval := time.Second
		slots := kubegresv1.ReplicationSlots{
			Enabled:                 true,
			DisableCleanup:          false,
			MaxWalKeepSize:          resource.MustParse("10Mi"),
			InactiveSlotGracePeriod: &disableGracePeriod,
			HealthCheckInterval:     healthCheckInterval,
		}

		test.givenExistingKubegresIsUpdatedWithReplicationSlotsSettings(slots)

		test.whenKubegresIsUpdated()

		test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
		test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

		test.thenEventShouldHaveOccurred(testSlotRemovedEvent, 5*healthCheckInterval, 100*time.Millisecond)

		replicationSlots := test.dbQueryTestCases.GetReplicationSlots()

		Expect(replicationSlots).To(HaveLen(1))

		Expect(replicationSlots).NotTo(ContainElement(replicationslot.ReplicationSlot{
			Name:   "test_slot",
			Active: false,
		}))
	})
})
