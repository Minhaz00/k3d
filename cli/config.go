package run

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/mitchellh/go-homedir"
	"github.com/olekukonko/tablewriter"
)

type cluster struct {
	name        string
	image       string
	status      string
	serverPorts []string
	server      types.Container
	workers     []types.Container
}

// createDirIfNotExists checks for the existence of a directory and creates it along with all required parents if not.
// It returns an error if the directory (or parents) couldn't be created and nil if it worked fine or if the path already exists.
func createDirIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, os.ModePerm)
	}
	return nil
}

// createClusterDir creates a directory with the cluster name under $HOME/.config/k3d/<cluster_name>.
// The cluster directory will be used e.g. to store the kubeconfig file.
func createClusterDir(name string) {
	clusterPath, _ := getClusterDir(name)
	if err := createDirIfNotExists(clusterPath); err != nil {
		log.Fatalf("ERROR: couldn't create cluster directory [%s] -> %+v", clusterPath, err)
	}
}

// deleteClusterDir contrary to createClusterDir, this deletes the cluster directory under $HOME/.config/k3d/<cluster_name>
func deleteClusterDir(name string) {
	clusterPath, _ := getClusterDir(name)
	if err := os.RemoveAll(clusterPath); err != nil {
		log.Printf("WARNING: couldn't delete cluster directory [%s]. You might want to delete it manually.", clusterPath)
	}
}

// getClusterDir returns the path to the cluster directory which is $HOME/.config/k3d/<cluster_name>
func getClusterDir(name string) (string, error) {
	//getting the home directory
	homeDir, err := homedir.Dir()
	if err != nil {
		log.Printf("ERROR: Couldn't get user's home directory")
		return "", err
	}
	// $HOME/.config/k3d/<cluster_name>
	return path.Join(homeDir, ".config", "k3d", name), nil
}

// printClusters prints the names of existing clusters
func printClusters(all bool) {
	// Retrieve the list of cluster names using getClusterNames
	clusters, err := getClusters()
	if err != nil {
		log.Fatalf("ERROR: Couldn't list clusters\n %+v", err)
	}

	if len(clusters) == 0 {
		log.Printf("No clusters found!")
		return
	}

	// Initialize a new tablewriter instance to create a formatted table for displaying cluster information.
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NAME", "IMAGE", "STATUS", "WORKERS"})

	for _, cluster := range clusters {
		workersRunning := 0
		for _, worker := range cluster.workers {
			if worker.State == "running" {
				workersRunning++
			}
		}
		workerData := fmt.Sprintf("%d/%d", workersRunning, len(cluster.workers))
		clusterData := []string{cluster.name, cluster.image, cluster.status, workerData}
		if cluster.status == "running" || all {
			table.Append(clusterData)
		}
	}

	// Render the table to display the formatted cluster information in the console
	table.Render()
}

func getClusters() (map[string]cluster, error) {

	// Creates a background context and initializes a Docker client
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return nil, fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	// Sets up Docker API filters (filters) to find containers with specific labels
	filters := filters.NewArgs()
	filters.Add("label", "app=k3d")
	filters.Add("label", "component=server")

	// List Server Containers (K3d Servers)
	k3dServers, err := docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, fmt.Errorf("WARNING: couldn't list server containers\n%+v", err)
	}

	// map for cluster [clusterName -> cluster struct]
	clusters := make(map[string]cluster)

	// don't filter for servers but for workers now
	filters.Del("label", "component=server")
	filters.Add("label", "component=worker")

	// for all servers created by k3d, get workers and cluster information
	for _, server := range k3dServers {
		filters.Add("label", fmt.Sprintf("cluster=%s", server.Labels["cluster"]))

		// retrieve a list of worker containers (workers)
		workers, err := docker.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filters,
		})
		if err != nil {
			log.Printf("WARNING: couldn't get worker containers for cluster %s\n%+v", server.Labels["cluster"], err)
		}

		// Extract server ports (serverPorts) from container port mappings (server.Ports)
		serverPorts := []string{}
		for _, port := range server.Ports {
			serverPorts = append(serverPorts, strconv.Itoa(int(port.PublicPort)))
		}

		// Populate cluster information (cluster) with relevant attributes
		clusters[server.Labels["cluster"]] = cluster{
			name:        server.Labels["cluster"],
			image:       server.Image,
			status:      server.State,
			serverPorts: serverPorts,
			server:      server,
			workers:     workers,
		}

		// clear label filters before searching for next cluster
		filters.Del("label", fmt.Sprintf("cluster=%s", server.Labels["cluster"]))
	}
	return clusters, nil
}

// getCluster creates a cluster struct with populated information fields
func getCluster(name string) (cluster, error) {
	clusters, err := getClusters()
	return clusters[name], err
}
