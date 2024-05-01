package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"unicode"

	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var (
	// Environment variables
	k8s_namespace    = os.Getenv("KUBE_NAMESPACE")   // Optional, for watching a specific namespace
	xc_namespace     = os.Getenv("XC_NAMESPACE")     // Required, the XC namespace for the API
	xc_token         = os.Getenv("XC_TOKEN")         // Required, the token for API authentication
	xc_sitename      = os.Getenv("XC_SITENAME")      // Required, the site name for the Origin Pool
	xc_siteinterface = os.Getenv("XC_SITEINTERFACE") // Required, the interface for the Site; Inside / Outside
	api_domain       = os.Getenv("API_DOMAIN")       // Required, the API domain in https://domain.com format
)

func main() {
	if xc_namespace == "" || xc_token == "" || api_domain == "" || xc_sitename == "" {
		log.Fatal("XC_NAMESPACE, XC_TOKEN, XC_SITENAME, and API_DOMAIN environment variables must be set")
	}

	clientset := getClientSet()
	watchServices(clientset, k8s_namespace)
}

func getClientSet() *kubernetes.Clientset {
	// Create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting in-cluster config: %s", err.Error())
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building Kubernetes clientset: %s", err.Error())
	}

	return clientset
}

func checkOriginPoolExists(clientset *kubernetes.Clientset, service *corev1.Service) (bool, error) {
	// Format the service name according to the specified rules
	formattedServiceName := formatServiceName(service.Name)

	// Construct the URL for the API call to check existence
	url := fmt.Sprintf("%s/api/config/namespaces/%s/origin_pools/%s", api_domain, xc_namespace, formattedServiceName)

	// Create the request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("error creating request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", xc_token))

	// Create a new HTTP client and send the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error sending request to API: %v", err)
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode == http.StatusOK {
		// Origin pool exists
		return true, nil
	} else if resp.StatusCode == http.StatusNotFound {
		// Origin pool does not exist
		return false, nil
	}

	// Other HTTP status codes (unexpected)
	return false, fmt.Errorf("unexpected HTTP status: %s", resp.Status)
}

func watchServices(clientset *kubernetes.Clientset, namespace string) {
	watchlist := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "services", namespace, fields.Everything())
	_, controller := cache.NewInformer(
		watchlist,
		&corev1.Service{},
		0, // Immediate resync
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				service, ok := obj.(*corev1.Service)
				if ok && service.Spec.Type == corev1.ServiceTypeNodePort {
					go manageOriginPool(clientset, service)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldService, okOld := oldObj.(*corev1.Service)
				newService, okNew := newObj.(*corev1.Service)
				if okOld && okNew && newService.Spec.Type == corev1.ServiceTypeNodePort {
					go manageOriginPool(clientset, newService)
				}
			},
			DeleteFunc: func(obj interface{}) {
				service, ok := obj.(*corev1.Service)
				if ok && service.Spec.Type == corev1.ServiceTypeNodePort {
					go deleteOriginPool(clientset, service)
				}
			},
		},
	)

	stop := make(chan struct{})
	go controller.Run(stop)
	<-stop
}

func getNodeIPsForService(clientset *kubernetes.Clientset, service *corev1.Service) ([]string, error) {
	var nodeIPs []string

	// Create a label selector from the map directly
	selector := labels.SelectorFromSet(service.Spec.Selector).String()

	// Use this selector to list pods
	pods, err := clientset.CoreV1().Pods(service.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("error fetching pods for service %s: %v", service.Name, err)
	}

	nodeNames := make(map[string]bool)
	for _, pod := range pods.Items {
		if _, exists := nodeNames[pod.Spec.NodeName]; !exists {
			node, err := clientset.CoreV1().Nodes().Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
			if err != nil {
				log.Printf("Error fetching node %s for pod %s: %v", pod.Spec.NodeName, pod.Name, err)
				continue
			}
			for _, addr := range node.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					nodeIPs = append(nodeIPs, addr.Address)
					nodeNames[pod.Spec.NodeName] = true
					break
				}
			}
		}
	}

	return nodeIPs, nil
}

func manageOriginPool(clientset *kubernetes.Clientset, service *corev1.Service) {
	exists, err := checkOriginPoolExists(clientset, service)
	if err != nil {
		log.Printf("Error checking if origin pool exists: %v", err)
		return
	}

	if exists {
		log.Printf("Origin pool already exists, updating: %s", service.Name)
		updateOriginPool(clientset, service) // Assume updateOriginPool is defined similarly
	} else {
		log.Printf("Creating new origin pool: %s", service.Name)
		createOriginPool(clientset, service)
	}
}

func createOriginPool(clientset *kubernetes.Clientset, service *corev1.Service) {
	// Get the node IPs where the service's pods are running
	nodeIPs, err := getNodeIPsForService(clientset, service)
	if err != nil {
		log.Printf("Error getting node IPs: %v", err)
		return
	}

	// Format the service name according to the specified rules
	formattedServiceName := formatServiceName(service.Name)

	// Construct the URL for the API call
	url := fmt.Sprintf("%s/api/config/namespaces/%s/origin_pools", api_domain, xc_namespace)

	// Build the origin servers slice dynamically based on the IPs
	originServers := make([]OriginServer, len(nodeIPs))
	for i, ip := range nodeIPs {
		originServers[i] = OriginServer{
			PrivateIP: PrivateIP{
				IP: ip,
				SiteLocator: SiteLocator{
					Site: Site{
						Tenant:    "f5-sa-rnxeudss",
						Namespace: "system",
						Name:      xc_sitename,
						Kind:      "site",
					},
				},
				InsideNetwork: map[string]interface{}{}, // Adjust based on xc_siteinterface
				// Add conditional population of InsideNetwork or OutsideNetwork
				OutsideNetwork: map[string]interface{}{}, // Use if xc_siteinterface == "Outside"
			},
		}
	}

	// Create the payload for the POST request
	payload := OriginPool{
		Metadata: Metadata{
			Name:        formattedServiceName, // Dynamically use the service name
			Description: "Created by OriginSync",
			Disable:     false,
		},
		Spec: Spec{
			OriginServers:         originServers,
			NoTLS:                 map[string]interface{}{},
			Port:                  3000, // Adjust as necessary
			SameAsEndpointPort:    map[string]interface{}{},
			LoadbalancerAlgorithm: "LB_OVERRIDE",
			EndpointSelection:     "LOCAL_PREFERRED",
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling payload: %v", err)
		return
	}

	// Create the request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("APIToken %s", xc_token))

	// Create a new HTTP client and send the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending request to API: %v", err)
		return
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to create origin pool: %s", resp.Status)
	} else {
		log.Println("Successfully created origin pool")
	}
}

func updateOriginPool(clientset *kubernetes.Clientset, service *corev1.Service) {
	// need to determin how we will name the origin pool, and check for existence before trying to update, and append name to URI

	// Get the node IPs where the service's pods are running
	nodeIPs, err := getNodeIPsForService(clientset, service)
	if err != nil {
		log.Printf("Error getting node IPs: %v", err)
		return
	}

	// Construct the URL for the API call
	url := fmt.Sprintf("%s/api/config/namespaces/%s/origin_pools", api_domain, xc_namespace)

	// Build the origin servers slice dynamically based on the IPs
	originServers := make([]OriginServer, len(nodeIPs))
	for i, ip := range nodeIPs {
		originServers[i] = OriginServer{
			PrivateIP: PrivateIP{
				IP: ip,
				SiteLocator: SiteLocator{
					Site: Site{
						Tenant:    "f5-sa-rnxeudss",
						Namespace: "system",
						Name:      xc_sitename,
						Kind:      "site",
					},
				},
				InsideNetwork: map[string]interface{}{}, // Adjust based on xc_siteinterface
				// Add conditional population of InsideNetwork or OutsideNetwork
				OutsideNetwork: map[string]interface{}{}, // Use if xc_siteinterface == "Outside"
			},
		}
	}

	// Create the payload for the POST request
	payload := OriginPool{
		Metadata: Metadata{
			Name:        service.Name, // Dynamically use the service name
			Description: "Created by OriginSync",
			Disable:     false,
		},
		Spec: Spec{
			OriginServers:         originServers,
			NoTLS:                 map[string]interface{}{},
			Port:                  3000, // Adjust as necessary
			SameAsEndpointPort:    map[string]interface{}{},
			LoadbalancerAlgorithm: "LB_OVERRIDE",
			EndpointSelection:     "LOCAL_PREFERRED",
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling payload: %v", err)
		return
	}

	// Create the request
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("APIToken %s", xc_token))

	// Create a new HTTP client and send the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending request to API: %v", err)
		return
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to create origin pool: %s", resp.Status)
	} else {
		log.Println("Successfully created origin pool")
	}
}

func deleteOriginPool(clientset *kubernetes.Clientset, service *corev1.Service) {
	// Logic to delete
}

func formatServiceName(serviceName string) string {
	// Replace periods with dashes
	formattedName := strings.ReplaceAll(serviceName, ".", "-")

	// Convert to lowercase
	formattedName = strings.ToLower(formattedName)

	// Ensure it starts with an alphabetic character
	for len(formattedName) > 0 && !unicode.IsLetter(rune(formattedName[0])) {
		formattedName = formattedName[1:]
	}

	// Remove invalid characters and ensure it ends with alphanumeric
	temp := ""
	for i, ch := range formattedName {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' {
			temp += string(ch)
		}
		// Ensure the last character is alphanumeric
		if i == len(formattedName)-1 && ch == '-' {
			temp = temp[:len(temp)-1]
		}
	}

	return temp
}
