package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
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

	// create cluster network
	networkID, err := createClusterNetwork(c.String("name"))
	if err != nil {
		return err
	}
	log.Printf("Created cluster network with ID %s", networkID)

	if c.IsSet("timeout") && !c.IsSet("wait") {
		return errors.New("cannot use --timeout flag without --wait flag")
	}

	// environment variables
	env := []string{"K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml"}
	if c.IsSet("env") || c.IsSet("e") {
		env = append(env, c.StringSlice("env")...)
	}
	k3sClusterSecret := ""
	if c.Int("workers") > 0 {
		k3sClusterSecret = fmt.Sprintf("K3S_CLUSTER_SECRET=%s", GenerateRandomString(20))
		env = append(env, k3sClusterSecret)
	}

	// k3s server arguments
	k3sServerArgs := []string{"--https-listen-port", c.String("port")}
	if c.IsSet("server-arg") || c.IsSet("x") {
		k3sServerArgs = append(k3sServerArgs, c.StringSlice("server-arg")...)
	}

	// createServer creates a container and returns the container Id
	log.Printf("Creating cluster [%s]", c.String("name"))
	dockerID, err := createServer(
		c.GlobalBool("verbose"),
		fmt.Sprintf("docker.io/rancher/k3s:%s", c.String("version")),
		c.String("port"),
		k3sServerArgs,
		env,
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

	// worker nodes
	if c.Int("workers") > 0 {
		k3sWorkerArgs := []string{}
		env := []string{k3sClusterSecret}
		log.Printf("Booting %s workers for cluster %s", strconv.Itoa(c.Int("workers")), c.String("name"))
		for i := 0; i < c.Int("workers"); i++ {
			workerID, err := createWorker(
				c.GlobalBool("verbose"),
				fmt.Sprintf("docker.io/rancher/k3s:%s", c.String("version")),
				k3sWorkerArgs,
				env,
				c.String("name"),
				strings.Split(c.String("volume"), ","),
				strconv.Itoa(i),
				c.String("port"),
			)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create worker node for cluster %s\n%+v", c.String("name"), err)
			}
			fmt.Printf("Created worker with ID %s\n", workerID)
		}
	}
	return nil
}

// DeleteCluster removes the cluster container and its cluster directory
func DeleteCluster(c *cli.Context) error {

	// operate on one or all clusters
	clusters := make(map[string]cluster)
	if !c.Bool("all") {
		cluster, err := getCluster(c.String("name"))
		if err != nil {
			return err
		}
		clusters[c.String("name")] = cluster
	} else {
		clusterMap, err := getClusters()
		if err != nil {
			return fmt.Errorf("ERROR: `--all` specified, but no clusters were found\n%+v", err)
		}
		// copy clusterMap
		for k, v := range clusterMap {
			clusters[k] = v
		}
	}

	// remove clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Removing cluster [%s]", cluster.name)

		// delete the workers of the cluster fisrt
		if len(cluster.workers) > 0 {
			log.Printf("...Removing %d workers\n", len(cluster.workers))
			for _, worker := range cluster.workers {
				if err := removeContainer(worker.ID); err != nil {
					log.Println(err)
					continue
				}
			}
		}

		log.Println("...Removing server")
		deleteClusterDir(cluster.name)
		if err := removeContainer(cluster.server.ID); err != nil {
			return fmt.Errorf("ERROR: Couldn't remove server for cluster %s\n%+v", cluster.name, err)
		}

		// delete the corresponding cluster network
		if err := deleteClusterNetwork(cluster.name); err != nil {
			log.Printf("WARNING: couldn't delete cluster network for cluster %s\n%+v", cluster.name, err)
		}

		log.Printf("SUCCESS: removed cluster [%s]", cluster.name)
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
		if err := docker.ContainerStop(ctx, cluster.server.ID, container.StopOptions{}); err != nil {
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
		if err := docker.ContainerStart(ctx, cluster.server.ID, container.StartOptions{}); err != nil {
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
	sourcePath := fmt.Sprintf("k3d-%s-server:/output/kubeconfig.yaml", c.String("name"))

	// Get the destination directory on the local host where the kubeconfig file will be copied
	destPath, _ := getClusterDir(c.String("name"))

	cmd := "docker"
	args := []string{"cp", sourcePath, destPath}

	if err := runCommand(c.GlobalBool("verbose"), cmd, args...); err != nil {
		return fmt.Errorf("ERROR: Couldn't get kubeconfig for cluster [%s]\n%+v", fmt.Sprintf("k3d-%s-server", c.String("name")), err)
	}

	// Prints the path of the copied kubeconfig file (kubeconfig.yaml) on the local host.
	fmt.Printf("%s\n", path.Join(destPath, "Kubeconfig.yaml"))
	return nil
}
