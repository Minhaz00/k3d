package run

/*
 * This file contains the "backend" functionality for the CLI commands (and flags)
 */

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/urfave/cli"
)

const (
	defaultRegistry    = "docker.io"
	defaultServerCount = 1
)


// CheckTools checks if the docker API server is responding
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

	if err := checkClusterName(c.String("name")); err != nil {
		return err
	}

	// define image
	image := c.String("image")
	if c.IsSet("version") {
		// TODO: --version to be deprecated
		log.Println("[WARNING] The `--version` flag will be deprecated soon, please use `--image rancher/k3s:<version>` instead")
		if c.IsSet("image") {
			// version specified, custom image = error (to push deprecation of version flag)
			log.Fatalln("[ERROR] Please use `--image <image>:<version>` instead of --image and --version")
		} else {
			// version specified, default image = ok (until deprecation of version flag)
			image = fmt.Sprintf("%s:%s", strings.Split(image, ":")[0], c.String("version"))
		}
	}
	if len(strings.Split(image, "/")) <= 2 {
		// fallback to default registry
		image = fmt.Sprintf("%s/%s", defaultRegistry, image)
	}

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
	k3sToken := ""
	if c.Int("workers") > 0 {
		k3sClusterSecret = fmt.Sprintf("K3S_CLUSTER_SECRET=%s", GenerateRandomString(20))
		env = append(env, k3sClusterSecret)

		k3sToken = fmt.Sprintf("K3S_TOKEN=%s", GenerateRandomString(20))
		env = append(env, k3sToken)
	}

	// k3s server arguments
	// TODO: --port will soon be --api-port since we want to re-use --port for arbitrary port mappings
	if c.IsSet("port") {
		log.Println("WARNING: As of v2.0.0 --port will be used for arbitrary port-mappings. It's original functionality can then be used via --api-port.")
	}

	k3sServerArgs := []string{"--https-listen-port", c.String("api-port")}

	if c.IsSet("server-arg") || c.IsSet("x") {
		k3sServerArgs = append(k3sServerArgs, c.StringSlice("server-arg")...)
	}

	// new port map
	// protmap ==> map[string][]string  ==> key: node-name, value: slice of portSpec
	portmap, err := mapNodesToPortSpecs(c.StringSlice("publish"), GetAllContainerNames(c.String("name"), defaultServerCount, c.Int("workers")))
	if err != nil {
		log.Fatal(err)
	}

	// createServer creates a container and returns the container Id
	log.Printf("Creating cluster [%s]", c.String("name"))
	dockerID, err := createServer(
		c.GlobalBool("verbose"),
		image,
		c.String("api-port"),
		k3sServerArgs,
		env,
		c.String("name"),
		strings.Split(c.String("volume"), ","),
		portmap,
	)
	if err != nil {
		log.Printf("ERROR: failed to create cluster\n%+v", err)
		delErr := DeleteCluster(c)
		if delErr != nil {
			return delErr
		}
		os.Exit(1)
	}

	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	// Wait for k3s to be up and running if wanted.
	// We're simply scanning the container logs for a line that tells us that everything's up and running
	// TODO: also wait for worker nodes
	start := time.Now()
	// Retrieve the timeout duration from the command-line flags and convert it to a time.Duration
	timeout := time.Duration(c.Int("timeout")) * time.Second
	// Loop continues as long as the "wait" flag is set in the command-line context (c)
	for c.IsSet("wait") {
		// not running after timeout exceeded? Rollback and delete everything.
		if timeout != 0 && !time.Now().After(start.Add(timeout)) {
			// If timeout is reached, attempt to delete the cluster and handle any error
			err := DeleteCluster(c)
			if err != nil {
				return err
			}
			return errors.New("cluster creation exceeded specified timeout")
		}

		// scan container logs for a line that tells us that the required services are up and running
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

	// create the directory where we will put the kubeconfig file by default (when running `k3d get-config`)
	// TODO: this can probably be moved to `k3d get-config` or be removed in a different approach
	createClusterDir(c.String("name"))

	// spin up the worker nodes
	// TODO: do this concurrently in different goroutines
	if c.Int("workers") > 0 {
		k3sWorkerArgs := []string{}
		env := []string{k3sClusterSecret, k3sToken}
		log.Printf("Booting %s workers for cluster %s", strconv.Itoa(c.Int("workers")), c.String("name"))
		for i := 0; i < c.Int("workers"); i++ {
			workerID, err := createWorker(
				c.GlobalBool("verbose"),
				image,
				k3sWorkerArgs,
				env,
				c.String("name"),
				strings.Split(c.String("volume"), ","),
				i,
				c.String("api-port"),
				portmap,
				c.Int("port-auto-offset"),
			)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create worker node for cluster %s\n%+v", c.String("name"), err)
			}
			log.Printf("Created worker with ID %s\n", workerID)
		}
	}

	log.Printf("SUCCESS: created cluster [%s]", c.String("name"))
	log.Printf(`You can now use the cluster with: 
	export KUBECONFIG="$(%s get-kubeconfig --name='%s')" 
	kubectl cluster-info`, os.Args[0], c.String("name"))

	return nil
}

// DeleteCluster removes the containers belonging to a cluster and its local directory
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
			// TODO: this could be done in goroutines
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

	// operate on one or all cluster
	clusters := make(map[string]cluster)
	if !c.Bool("all") {
		cluster, err := getCluster((c.String("name")))
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
		for key, value := range clusterMap {
			clusters[key] = value
		}
	}

	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	// stop clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Stopping cluster [%s]", cluster.name)
		if len(cluster.workers) > 0 {
			log.Printf("...Stopping %d workers\n", len(cluster.workers))
			for _, worker := range cluster.workers {
				if err := docker.ContainerStop(ctx, worker.ID, container.StopOptions{}); err != nil {
					log.Println(err)
					continue
				}
			}
		}
		log.Println("...Stopping server")
		if err := docker.ContainerStop(ctx, cluster.server.ID, container.StopOptions{}); err != nil {
			return fmt.Errorf("ERROR: Couldn't stop server for cluster %s\n%+v", cluster.name, err)
		}

		log.Printf("SUCCESS: Stopped cluster [%s]", cluster.name)
	}
	return nil
}

// StartCluster starts a stopped cluster container
func StartCluster(c *cli.Context) error {

	// operate on one or all cluster
	clusters := make(map[string]cluster)
	if !c.Bool("all") {
		cluster, err := getCluster((c.String("name")))
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
		for key, value := range clusterMap {
			clusters[key] = value
		}
	}

	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create docker client\n%+v", err)
	}

	// start clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Starting cluster [%s]", cluster.name)

		log.Println("...Starting server")
		if err := docker.ContainerStart(ctx, cluster.server.ID, container.StartOptions{}); err != nil {
			return fmt.Errorf("ERROR: Couldn't start server for cluster %s\n%+v", cluster.name, err)
		}

		if len(cluster.workers) > 0 {
			log.Printf("...Starting %d workers\n", len(cluster.workers))
			for _, worker := range cluster.workers {
				if err := docker.ContainerStart(ctx, worker.ID, container.StartOptions{}); err != nil {
					log.Println(err)
					continue
				}
			}
		}

		log.Printf("SUCCESS: Started cluster [%s]", cluster.name)
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
	ctx := context.Background()
	docker, err := client.NewClientWithOpts()
	if err != nil {
		return err
	}

	filters := filters.NewArgs()
	filters.Add("label", "app=k3d")
	filters.Add("label", fmt.Sprintf("cluster=%s", c.String("name")))
	filters.Add("label", "component=server")
	server, err := docker.ContainerList(ctx, container.ListOptions{
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to get server container for cluster %s\n%+v", c.String("name"), err)
	}

	if len(server) == 0 {
		return fmt.Errorf("no server container for cluster %s", c.String("name"))
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
	clusterDir, err := getClusterDir(c.String("name"))
	destPath := fmt.Sprintf("%s/kubeconfig.yaml", clusterDir)
	if err != nil {
		return err
	}

	kubeconfigfile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("ERROR: couldn't create kubeconfig.yaml in %s\n%+v", clusterDir, err)
	}
	defer kubeconfigfile.Close()

	// write to file, skipping the first 512 bytes which contain file metadata and trimming any NULL characters
	_, err = kubeconfigfile.Write(bytes.Trim(readBytes[512:], "\x00"))
	if err != nil {
		return fmt.Errorf("ERROR: couldn't write to kubeconfig.yaml\n%+v", err)
	}

	// output kubeconfig file path to stdout
	fmt.Println(destPath)

	return nil
}
