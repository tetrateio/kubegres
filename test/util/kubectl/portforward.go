package kubectl

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	portforwardtools "k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/kubectl/pkg/cmd/portforward"
)

// PortForwardOptions contains all the options for running the port-forward cli command.
//type PortForwardOptions struct {
//	Namespace     string
//	PodName       string
//	RESTClient    *restclient.RESTClient
//	Config        *restclient.Config
//	PodClient     corev1client.PodsGetter
//	Address       []string
//	Ports         []string
//	PortForwarder portForwarder
//	StopChannel   chan struct{}
//	ReadyChannel  chan struct{}
//}

func NewPortForwardOptions(namespace, podName string,
	podClient corev1client.PodsGetter, restClient *restclient.RESTClient, config *restclient.Config) *portforward.PortForwardOptions {

	return &portforward.PortForwardOptions{
		Namespace:  namespace,
		PodName:    podName,
		RESTClient: restClient,
		Config:     config,
		PodClient:  podClient,
		PortForwarder: &defaultPortForwarder{
			IOStreams: genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
		},
	}
}

//type portForwarder interface {
//	ForwardPorts(method string, url *url.URL, opts portforward.PortForwardOptions) error
//}

type defaultPortForwarder struct {
	genericclioptions.IOStreams
}

func (f *defaultPortForwarder) ForwardPorts(method string, url *url.URL, opts portforward.PortForwardOptions) error {
	transport, upgrader, err := spdy.RoundTripperFor(opts.Config)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, method, url)
	fmt.Printf("Forwarding pod to %v %v\n", opts.Address, opts.Ports)
	fw, err := portforwardtools.NewOnAddresses(dialer, opts.Address, opts.Ports, opts.StopChannel, opts.ReadyChannel, f.Out, f.ErrOut)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

//func (o PortForwardOptions) RunPortForward() error {
//	pod, err := o.PodClient.Pods(o.Namespace).Get(context.TODO(), o.PodName, metav1.GetOptions{})
//	if err != nil {
//		return err
//	}
//
//	if pod.Status.Phase != corev1.PodRunning {
//		return fmt.Errorf("unable to forward port because pod is not running. Current status=%v", pod.Status.Phase)
//	}
//
//	signals := make(chan os.Signal, 1)
//	signal.Notify(signals, os.Interrupt)
//	defer signal.Stop(signals)
//
//	go func() {
//		<-signals
//		if o.StopChannel != nil {
//			close(o.StopChannel)
//		}
//	}()
//
//	req := o.RESTClient.Post().
//		Resource("pods").
//		Namespace(o.Namespace).
//		Name(pod.Name).
//		SubResource("portforward")
//
//	return o.PortForwarder.ForwardPorts("POST", req.URL(), o)
//}
