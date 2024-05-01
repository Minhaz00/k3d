package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

const (
	defaultContainerNamePrefix = "k3d"
)

type cluster struct {
	name        string
	image       string
	status      string
	serverPorts []string
	server      types.Container
	workers     []types.Container
}

// GetContainerName generates the container names
func GetContainerName(role, clusterName string, postfix int) string {
	if postfix >= 0 {
		return fmt.Sprintf("%s-%s-%s-%d", defaultContainerNamePrefix, clusterName, role, postfix)
	}

	// for server
	return fmt.Sprintf("%s-%s-%s", defaultContainerNamePrefix, clusterName, role)
}

// GetAllContainerNames returns a list of all containernames that will be created
func GetAllContainerNames(clusterName string, serverCount, workerCount int) []string {
	names := []string{}
	for postfix := 0; postfix < serverCount; postfix++ {
		names = append(names, GetContainerName("server", clusterName, postfix))
	}
	for postfix := 0; postfix < workerCount; postfix++ {
		names = append(names, GetContainerName("worker", clusterName, postfix))
	}
	return names
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

func getClusterKubeConfigPath(cluster string) (string, error) {
	clusterDir, err := getClusterDir(cluster)
	return path.Join(clusterDir, "kubeconfig.yaml"), err
}

func createKubeConfigFile(cluster string) error {
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return err
	}

	filters := filters.NewArgs()
	filters.Add("label", "app=k3d")
	filters.Add("label", fmt.Sprintf("cluster=%s", cluster))
	filters.Add("label", "component=server")
	server, err := docker.ContainerList(ctx, container.ListOptions{
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to get server container for cluster %s\n%+v", cluster, err)
	}

	if len(server) == 0 {
		return fmt.Errorf("no server container for cluster %s", cluster)
	}

	// get kubeconfig file from container
	reader, _, err := docker.CopyFromContainer(ctx, server[0].ID, "/output/kubeconfig.yaml")
	if err != nil {
		return fmt.Errorf("ERROR: couldn't copy kubeconfig.yaml from server container %s\n%+v", server[0].ID, err)
	}
	defer reader.Close()

	// read contents of that file
	readBytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("ERROR: couldn't read kubeconfig from container\n%+v", err)
	}

	// create destination kubeconfig file
	destPath, err := getClusterKubeConfigPath(cluster)
	if err != nil {
		return err
	}

	kubeconfigfile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create kubeconfig.yaml in %s\n%+v", destPath, err)
	}
	defer kubeconfigfile.Close()

	// write to file, skipping the first 512 bytes which contain file metadata and trimming any NULL characters
	_, err = kubeconfigfile.Write(bytes.Trim(readBytes[512:], "\x00"))
	if err != nil {
		return fmt.Errorf("ERROR: couldn't write to kubeconfig.yaml\n%+v", err)
	}

	return nil
}

func getKubeConfig(cluster string) (string, error) {
	kubeConfigPath, err := getClusterKubeConfigPath(cluster)
	if err != nil {
		return "", err
	}

	if clusters, err := getClusters(false, cluster); err != nil || len(clusters) != 1 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("Cluster %s does not exist", cluster)
	}

	// If kubeconfig.yaml has not been created, generate it now
	if _, err := os.Stat(kubeConfigPath); os.IsNotExist(err) {
		if err = createKubeConfigFile(cluster); err != nil {
			return "", err
		}
	} else {
		return "", err
	}

	return kubeConfigPath, nil

}

// printClusters prints the names of existing clusters
func printClusters() {
	// Retrieve the list of cluster names using getClusterNames
	clusters, err := getClusters(true, "")
	if err != nil {
		log.Fatalf("ERROR: Couldn't list clusters\n %+v", err)
	}

	if len(clusters) == 0 {
		log.Printf("No clusters found!")
		return
	}

	// Initialize a new tablewriter instance to create a formatted table for displaying cluster information.
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_CENTER)
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
		table.Append(clusterData)
	}

	// Render the table to display the formatted cluster information in the console
	table.Render()

}

// When 'all' is true, 'cluster' contains all clusters found from the docker daemon
// When 'all' is false, 'cluster' contains up to one cluster whose name matches 'name'. 'cluster' can
// be empty if no matching cluster is found.
func getClusters(all bool, name string) (map[string]cluster, error) {

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
		clusterName := server.Labels["cluster"]

		// Skip the cluster if we don't want all of them, and
		// the cluster name does not match.
		if all || name == clusterName {
			// Add the cluster
			filters.Add("label", fmt.Sprintf("cluster=%s", clusterName))

			// retrieve a list of worker containers (workers)
			workers, err := docker.ContainerList(ctx, container.ListOptions{
				All:     true,
				Filters: filters,
			})
			if err != nil {
				log.Printf("WARNING: couldn't get worker containers for cluster %s\n%+v", clusterName, err)
			}

			// Extract server ports (serverPorts) from container port mappings (server.Ports)
			serverPorts := []string{}
			for _, port := range server.Ports {
				serverPorts = append(serverPorts, strconv.Itoa(int(port.PublicPort)))
			}

			// Populate cluster information (cluster) with relevant attributes
			clusters[clusterName] = cluster{
				name:        clusterName,
				image:       server.Image,
				status:      getClusterStatus(server, workers),
				serverPorts: serverPorts,
				server:      server,
				workers:     workers,
			}

			// clear label filters before searching for next cluster
			filters.Del("label", fmt.Sprintf("cluster=%s", clusterName))
		}
	}

	return clusters, nil
}

// Classify cluster state: Running, Stopped or Abnormal
func getClusterStatus(server types.Container, workers []types.Container) string {
	// The cluster is in the abnromal state when server state and the worker states don't agree
	for _, w := range workers {
		if w.State != server.State {
			return "unhealthy"
		}
	}

	switch server.State {
	// All containers in this state are most likely as the result of running the "k3d stop" command
	case "exited":
		return "stopped"
	}

	return server.State
}
