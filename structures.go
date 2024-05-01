package main

type OriginPool struct {
	Metadata Metadata `json:"metadata"`
	Spec     Spec     `json:"spec"`
}

type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Disable     bool   `json:"disable"`
}

type Spec struct {
	OriginServers         []OriginServer         `json:"origin_servers"`
	NoTLS                 map[string]interface{} `json:"no_tls"`
	Port                  int                    `json:"port"`
	SameAsEndpointPort    map[string]interface{} `json:"same_as_endpoint_port"`
	LoadbalancerAlgorithm string                 `json:"loadbalancer_algorithm"`
	EndpointSelection     string                 `json:"endpoint_selection"`
}

type OriginServer struct {
	PrivateIP PrivateIP `json:"private_ip"`
}

type PrivateIP struct {
	IP             string                 `json:"ip"`
	SiteLocator    SiteLocator            `json:"site_locator"`
	InsideNetwork  map[string]interface{} `json:"inside_network"`
	OutsideNetwork map[string]interface{} `json:"outside_network"`
}

type SiteLocator struct {
	Site Site `json:"site"`
}

type Site struct {
	Tenant    string `json:"tenant"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
}
