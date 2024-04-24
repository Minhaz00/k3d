package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/urfave/cli"
)

// CheckTools checks if the installed tools work correctly
func CheckTools(c *cli.Context) error {
	log.Print("Checking docker...")

	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return err
	}
	
	// Ping the Docker daemon to check its availability
	ping, err := docker.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ERROR: checking docker failed\n%+v", err)
	}

	// Log the success message with Docker API version
	log.Printf("SUCCESS: Checking docker succeeded (API: v%s)\n", ping.APIVersion)
	return nil
}

// CreateCluster creates a new single-node cluster container and initializes the cluster directory
func CreateCluster(c *cli.Context) error {

	if c.IsSet("timeout") && !c.IsSet("wait") {
		return errors.New("cannot use --timeout flag without --wait flag")
	}

	// k3s server arguments
	k3sServerArgs := []string{"--https-listen-port", c.String("port")}
	if c.IsSet("server-arg") || c.IsSet("x") {
		k3sServerArgs = append(k3sServerArgs, c.StringSlice("server-arg")...)
	}


	log.Printf("Creating cluster [%s]", c.String("name"))
	// createServer creates a container and returns the container Id
	dockerID, err := createServer(
		c.Bool("verbose"),
		fmt.Sprintf("docker.io/rancher/k3s:%s", c.String("version")),
		c.String("port"),
		k3sServerArgs,
		[]string{"K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml"},
		c.String("name"),
		strings.Split(c.String("volume"), ","),
	)
	if err != nil {
		log.Fatalf("ERROR: failed to create cluster\n%+v", err)
	}

	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	
	// wait for k3s to be up and running if we want it
	// Get the current time to use as a reference point
	start := time.Now()
	// Retrieve the timeout duration from the command-line flags and convert it to a time.Duration
	timeout := time.Duration(c.Int("timeout")) * time.Second
	// Loop continues as long as the "wait" flag is set in the command-line context (c)
	for c.IsSet("wait") {
		// Check if the timeout duration has not elapsed and the current time is before the timeout deadline
		if timeout != 0 && !time.Now().After(start.Add(timeout)) {
			// If timeout is reached, attempt to delete the cluster and handle any error
			err := DeleteCluster(c)
			if err != nil {
				return err
			}
			return errors.New("cluster creation exceeded specified timeout")
		}

		out, err := docker.ContainerLogs(ctx, dockerID, container.LogsOptions{
			ShowStdout: true, 
			ShowStderr: true,
		})
		if err != nil {
			out.Close()
			return fmt.Errorf("ERROR: couldn't get docker logs for %s\n%+v", c.String("name"), err)
		}

		// Read logs into a buffer and close the log stream
		buf := new(bytes.Buffer)
		nRead, _ := buf.ReadFrom(out)
		out.Close()
		output := buf.String()

		// Check if logs contain "Running kubelet" indicating the cluster is up
		if nRead > 0 && strings.Contains(string(output), "Running kubelet") {
			break
		}

		// Sleep for 1 second before checking logs again
		time.Sleep(1 * time.Second)
	}

	createClusterDir(c.String("name"))
	log.Printf("SUCCESS: created cluster [%s]", c.String("name"))
	log.Printf(`You can now use the cluster with: 
	export KUBECONFIG="$(%s get-kubeconfig --name='%s')" 
	kubectl cluster-info`, os.Args[0], c.String("name"))
	return nil
}


// DeleteCluster removes the cluster container and its cluster directory
func DeleteCluster(c *cli.Context) error {
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	clusterNames := []string{}

	// operate on one or all clusters
	if !c.Bool("all") {
		clusterNames = append(clusterNames, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			return fmt.Errorf("ERROR: `--all` specified, but no clusters were found\n%+v", err)
		}
		clusterNames = append(clusterNames, clusterList...)
	}

	// remove clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, name := range clusterNames {
		log.Printf("Removing cluster [%s]", name)
		
		cluster, err := getCluster(name)
		if err != nil {
			log.Printf("WARNING: couldn't get docker info for %s", name)
			continue
		}

		if err := docker.ContainerRemove(ctx, cluster.id, container.RemoveOptions{}); err != nil {
			log.Printf("WARNING: couldn't delete cluster [%s], trying a force remove now.", cluster.name)
			if err := docker.ContainerRemove(ctx, cluster.id, container.RemoveOptions{Force: true}); err != nil {
				log.Printf("FAILURE: couldn't delete cluster container for [%s] -> %+v", cluster.name, err)
			}
		}

		deleteClusterDir(cluster.name)
		log.Printf("SUCCESS: removed cluster [%s]", name)
	}
	return nil
}

// StopCluster stops a running cluster container (restartable)
func StopCluster(c *cli.Context) error {
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	clusterNames := []string{}

	// operate on one or all clusters
	if !c.Bool("all") {
		clusterNames = append(clusterNames, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			return fmt.Errorf("ERROR: `--all` specified, but no clusters were found\n%+v", err)
		}
		clusterNames = append(clusterNames, clusterList...)
	}

	// stop clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, name := range clusterNames {
		log.Printf("Stopping cluster [%s]", name)
		cluster, err := getCluster(name)
		if err != nil {
			log.Printf("WARNING: couldn't get docker info for %s", name)
			continue
		}
		if err := docker.ContainerStop(ctx, cluster.id, container.StopOptions{}); err != nil {
			fmt.Printf("WARNING: couldn't stop cluster %s\n%+v", cluster.name, err)
			continue
		}
		log.Printf("SUCCESS: stopped cluster [%s]", cluster.name)
	}
	return nil
}

// StartCluster starts a stopped cluster container
func StartCluster(c *cli.Context) error {
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	clusterNames := []string{}


	// operate on one or all clusters
	if !c.Bool("all") {
		clusterNames = append(clusterNames, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			return fmt.Errorf("ERROR: `--all` specified, but no clusters were found\n%+v", err)
		}
		clusterNames = append(clusterNames, clusterList...)
	}

	// start clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, name := range clusterNames {
		log.Printf("Starting cluster [%s]", name)
		cluster, err := getCluster(name)
		if err != nil {
			log.Printf("WARNING: couldn't get docker info for %s", name)
			continue
		}
		if err := docker.ContainerStart(ctx, cluster.id, container.StartOptions{}); err != nil {
			fmt.Printf("WARNING: couldn't start cluster %s\n%+v", cluster.name, err)
			continue
		}
		log.Printf("SUCCESS: started cluster [%s]", cluster.name)
	}
	return nil
}

// ListClusters prints a list of created clusters
func ListClusters(c *cli.Context) error {
	printClusters(c.Bool("all"))
	return nil
}


// getKubeConfig grabs the kubeconfig from the running cluster and prints the path to stdout
func GetKubeConfig(c *cli.Context) error {
	// Construct the source path within the Docker container where the kubeconfig file is located
	sourcePath := fmt.Sprintf("%s:/output/kubeconfig.yaml", c.String("name"))

	// Get the destination directory on the local host where the kubeconfig file will be copied
	destPath, _ := getClusterDir(c.String("name"))

	cmd := "docker"
	args := []string{"cp", sourcePath, destPath}

	if err := runCommand(c.GlobalBool("verbose"), cmd, args...); err != nil {
		return fmt.Errorf("ERROR: Couldn't get kubeconfig for cluster [%s]\n%+v", fmt.Sprintf("%s-server", c.String("name")), err)
	}

	// Prints the path of the copied kubeconfig file (kubeconfig.yaml) on the local host.
	fmt.Printf("%s\n", path.Join(destPath, "Kubeconfig.yaml"))
	return nil
}  