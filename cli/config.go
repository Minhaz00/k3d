package run

import (
	"context"
	"log"
	"os"
	"path"

	dockerClient "github.com/docker/docker/client"
	"github.com/mitchellh/go-homedir"
	"github.com/olekukonko/tablewriter"
)

type cluster struct {
	name   string
	image  string
	status string
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
	clusterNames, err := getClusterNames()
	if err != nil {
		log.Fatalf("ERROR: Couldn't list clusters -> %+v", err)
	}

	if len(clusterNames) == 0 {
		log.Printf("No clusters found!")
		return
	}

	// Initialize a new tablewriter instance to create a formatted table for displaying cluster information.
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NAME", "IMAGE", "STATUS"})

	for _, clusterName := range clusterNames {
		cluster, _ := getCluster(clusterName)
		clusterData := []string{cluster.name, cluster.image, cluster.status}
		if cluster.status == "running" || all {
			table.Append(clusterData)
		}
	}
	// Render the table to display the formatted cluster information in the console
	table.Render()
}

// getClusterNames returns a list of cluster names which are folder names in the config directory
func getClusterNames() ([]string, error) {
	// Get the user's home directory
	homeDir, err := homedir.Dir()
	if err != nil {
		log.Printf("ERROR: Couldn't get user's home directory")
		return nil, err
	}

	// Construct the path to the k3d configuration directory within the user's home directory
	configDir := path.Join(homeDir, ".config", "k3d")

	// Read the contents of the k3d configuration directory
	files, err := os.ReadDir(configDir)
	if err != nil {
		log.Printf("ERROR: Couldn't list files in [%s]", configDir)
		return nil, err
	}

	// Initialize a slice to hold cluster names
	clusters := []string{}

	// Iterate through the files/directories in the config directory
	for _, file := range files {
		// checking if the file is a directory. as we are returning the names of the clusters, we only need to check for directories
		if file.IsDir() {
			clusters = append(clusters, file.Name())
		}
	}
	return clusters, nil
}


func getCluster(name string) (cluster, error) {
	cluster := cluster{
		name: name,
		image: "UNKNOWN",
		status: "UNKNOWN",
	}

	docker, err := dockerClient.NewClientWithOpts()
	if err != nil {
		log.Printf("ERROR: couldn't create docker client -> %+v", err)
		return cluster, err
	}

	containerInfo, err := docker.ContainerInspect(context.Background(), cluster.name)
	if err != nil {
		log.Printf("WARNING: couldn't get docker info for [%s] -> %+v", cluster.name, err)
	} else {
		cluster.image = containerInfo.Config.Image
		cluster.status = containerInfo.ContainerJSONBase.State.Status
	}

	return cluster, nil
}