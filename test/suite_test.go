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
	"context"
	"log"
	"path/filepath"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"reactive-tech.io/kubegres/controllers"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/kindcluster"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	postgresv1 "reactive-tech.io/kubegres/api/v1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var kindCluster kindcluster.KindTestClusterUtil

// var cfgTest *rest.Config
var k8sClientTest client.Client
var testEnv *envtest.Environment
var eventRecorderTest util.MockEventRecorderTestUtil

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {

	kindCluster.StartCluster()

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	log.Print("START OF: BeforeSuite")

	By("bootstrapping test environment")
	useExistingCluster := true
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &useExistingCluster,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = postgresv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClientTest, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClientTest).ToNot(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	mockLogger := util.CreateMockLogger()
	eventRecorderTest = util.MockEventRecorderTestUtil{}

	err = (&controllers.KubegresReconciler{
		Client:   k8sManager.GetClient(),
		Logger:   mockLogger,
		Scheme:   k8sManager.GetScheme(),
		Recorder: record.EventRecorder(&eventRecorderTest),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		if err != nil {
			log.Fatal("ERROR while starting Kubernetes: ", err)
		}
		Expect(err).ToNot(HaveOccurred())
	}()

	log.Println("Waiting for Kubernetes to start")

	k8sClientTest = k8sManager.GetClient()
	Expect(k8sClientTest).ToNot(BeNil())

	// Wait for Kubernetes envtest to start
	Eventually(func() error { return k8sClientTest.List(context.Background(), &v1.NamespaceList{}) },
		time.Second*30, time.Second*1).Should(Succeed())

	log.Println("Kubernetes has started")

	log.Print("END OF: BeforeSuite")
})

var _ = AfterSuite(func() {

	log.Print("START OF: Suite AfterSuite")

	By("tearing down the test environment")

	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())

	time.Sleep(5 * time.Second)

	kindCluster.DeleteCluster()

	log.Print("END OF: Suite AfterSuite")
})
