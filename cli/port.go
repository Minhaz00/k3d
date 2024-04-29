package run

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/go-connections/nat"
)

// PublishedPorts is a struct used for exposing container ports on the host system
type PublishedPorts struct {
	ExposedPorts map[nat.Port]struct{}
	PortBindings map[nat.Port][]nat.PortBinding
}

// mapping a node role to groups that should be applied to it
var nodeRuleGroupsMap = map[string][]string{
	"worker": []string{"all", "workers"},
	"server": []string{"all", "server", "master"},
}

// defaultNodes describes the type of nodes on which a port should be exposed by default
const defaultNodes = "all"

// mapNodesToPortSpecs maps nodes to portSpecs
func mapNodesToPortSpecs(specs []string) (map[string][]string, error) {

	if err := validatePortSpecs(specs); err != nil {
		return nil, err
	}

	nodeToPortSpecMap := make(map[string][]string)

	for _, spec := range specs {
		nodes, portSpec := extractNodes(spec)

		for _, node := range nodes {
			nodeToPortSpecMap[node] = append(nodeToPortSpecMap[node], portSpec)
		}
	}

	fmt.Printf("nodeToPortSpecMap: %+v\n", nodeToPortSpecMap)

	return nodeToPortSpecMap, nil
}

// The factory function for PublishedPorts
// creating a PublishedPorts struct based on the provided port specifications
// Parameters:
//   - specs []string: A slice of strings representing the port specifications. Each string should follow the format:
//     <host>:<hostPort>:<containerPort>[/<protocol>][@<node>]
//   - <host> is an optional hostname or IP address.
//   - <hostPort> is an optional host port number.
//   - <containerPort> is the container port number.
//   - <protocol> is an optional protocol (either "udp" or "tcp").
//   - <node> is an optional node name or role.
func CreatePublishedPorts(specs []string) (*PublishedPorts, error) {
	// If no port specifications are provided, it creates a default PublishedPorts with an empty ExposedPorts and PortBindings map.
	if len(specs) == 0 {
		var newExposedPorts = make(map[nat.Port]struct{}, 1)
		var newPortBindings = make(map[nat.Port][]nat.PortBinding, 1)
		return &PublishedPorts{ExposedPorts: newExposedPorts, PortBindings: newPortBindings}, nil
	}

	newExposedPorts, newPortBindings, err := nat.ParsePortSpecs(specs)
	return &PublishedPorts{ExposedPorts: newExposedPorts, PortBindings: newPortBindings}, err
}

// validatePortSpecs matches the provided port specs against a set of rules to enable early exit if something is wrong
// It checks if the specification matches the following format:
// <host>:<hostPort>:<containerPort>[/<protocol>][@<node>]*
// Example ==> specs := []string{"192.168.0.1:8080:80", "3000/tcp", "@node1:8080:80"}
func validatePortSpecs(specs []string) error {
	// regex matching (no sophisticated IP matching at the moment)
	regex := regexp.MustCompile(`^(((?P<ip>[\d\.]+)?:)?((?P<hostPort>[0-9]{0,6}):)?(?P<containerPort>[0-9]{1,6}))((/(?P<protocol>udp|tcp))?(?P<nodes>(@(?P<node>[\w-]+))+))$`)
	for _, spec := range specs {
		if !regex.MatchString(spec) {
			return fmt.Errorf("[ERROR] Provided port spec [%s] didn't match format specification (`[ip:][host-port:]container-port[/protocol]@node-specifier`)", spec)
		}
	}
	return nil
}

// extractNodes separates the node specification from the actual port specs
// Example 1:
//
//	nodes, portSpec := extractNodes("@node1:8080:80")
//	// nodes: [node1]
//	// portSpec: "8080:80"
//
//	nodes, portSpec := extractNodes("192.168.0.1:8080:80")
//	// nodes: [default]
//	// portSpec: "192.168.0.1:8080:80"
func extractNodes(spec string) ([]string, string) {
	// extract nodes
	nodes := []string{}
	atSplit := strings.Split(spec, "@")
	portSpec := atSplit[0]
	if len(atSplit) > 1 {
		nodes = atSplit[1:]
	}
	if len(nodes) == 0 {
		nodes = append(nodes, defaultNodes)
	}
	return nodes, portSpec
}

// Offset creates a new PublishedPort structure, with all host ports are changed by a fixed  'offset'
func (p PublishedPorts) Offset(offset int) *PublishedPorts {
	var newExposedPorts = make(map[nat.Port]struct{}, len(p.ExposedPorts))
	var newPortBindings = make(map[nat.Port][]nat.PortBinding, len(p.PortBindings))

	for k, v := range p.ExposedPorts {
		newExposedPorts[k] = v
	}

	for k, v := range p.PortBindings {
		bindings := make([]nat.PortBinding, len(v))
		for i, b := range v {
			port, _ := nat.ParsePort(b.HostPort)
			bindings[i].HostIP = b.HostIP
			bindings[i].HostPort = fmt.Sprintf("%d", port+offset)
		}
		newPortBindings[k] = bindings
	}

	return &PublishedPorts{ExposedPorts: newExposedPorts, PortBindings: newPortBindings}
}

// AddPort creates a new PublishedPort struct with one more port, based on 'portSpec'
func (p *PublishedPorts) AddPort(portSpec string) (*PublishedPorts, error) {
	portMappings, err := nat.ParsePortSpec(portSpec)
	if err != nil {
		return nil, err
	}

	var newExposedPorts = make(map[nat.Port]struct{}, len(p.ExposedPorts)+1)
	var newPortBindings = make(map[nat.Port][]nat.PortBinding, len(p.PortBindings)+1)

	// Populate the new maps
	for k, v := range p.ExposedPorts {
		newExposedPorts[k] = v
	}

	for k, v := range p.PortBindings {
		newPortBindings[k] = v
	}

	// Add new ports
	for _, portMapping := range portMappings {
		port := portMapping.Port
		if _, exists := newExposedPorts[port]; !exists {
			newExposedPorts[port] = struct{}{}
		}

		bslice, exists := newPortBindings[port]
		if !exists {
			bslice = []nat.PortBinding{}
		}
		newPortBindings[port] = append(bslice, portMapping.Binding)
	}

	return &PublishedPorts{ExposedPorts: newExposedPorts, PortBindings: newPortBindings}, nil
}

// MergePortSpecs merges published ports for a given node
func MergePortSpecs(nodeToPortSpecMap map[string][]string, role, name string) ([]string, error) {

	portSpecs := []string{}

	// add portSpecs according to node role
	for _, group := range nodeRuleGroupsMap[role] {
		for _, v := range nodeToPortSpecMap[group] {
			exists := false
			for _, i := range portSpecs {
				if v == i {
					exists = true
				}
			}
			if !exists {
				portSpecs = append(portSpecs, v)
			}
		}
	}

	// add portSpecs according to node name
	for _, v := range nodeToPortSpecMap[name] {
		exists := false
		for _, i := range portSpecs {
			if v == i {
				exists = true
			}
		}
		if !exists {
			portSpecs = append(portSpecs, v)
		}
	}

	return portSpecs, nil
}
