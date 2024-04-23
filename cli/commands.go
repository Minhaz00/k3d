package run

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/urfave/cli"
)

// CheckTools checks if the installed tools work correctly
func CheckTools(c *cli.Context) error {
	log.Print("Checking docker...")
	cmd := "docker"
	args := []string{"version"}
	if err := runCommand(true, cmd, args...); err != nil {
		log.Fatalf("Checking docker: FAILED")
		return err
	}
	log.Println("Checking docker: SUCCESS")
	return nil
}

// CreateCluster creates a new single-node cluster container and initializes the cluster directory
func CreateCluster(c *cli.Context) error {

	if c.IsSet("timeout") && !c.IsSet("wait") {
		return errors.New("--wait flag is not specified")
	}

	port := fmt.Sprintf("%s:%s", c.String("port"), c.String("port"))
	image := fmt.Sprintf("rancher/k3s:%s", c.String("version"))
	cmd := "docker"
	args := []string{
		"run",
		"--name", c.String("name"),
		"-e", "K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml",
		"--publish", port,
		"--privileged",
	}

	//list of extra argument
	extraArgs := []string{}
	if c.IsSet("volume") {
		extraArgs = append(extraArgs, "--volume", c.String("volume"))
	}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}

	args = append(args,
		"-d",
		image,
		"server",
		"--https-listen-port", c.String("port"),
	)

	// run the command
	log.Printf("Creating cluster [%s]", c.String("name"))
	if err := runCommand(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't create cluster [%s] --> %+v", c.String("name"), err)
		return err
	}

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
			return errors.New("Cluster timeout expired")
		}

		// Construct a Docker command to retrieve logs of the specified container
		cmd := "docker"
		args = []string{
			"logs",
			c.String("name"),
		}
		prog := exec.Command(cmd, args...)
		output, err := prog.CombinedOutput()
		if err != nil {
			return err
		}

		// Check if the output of the Docker logs contains the substring "Running kubelet"
		// If substring is found, exit the loop indicating successful initialization
		if strings.Contains(string(output), "Running kubelet") {
			break
		}

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
	cmd := "docker"
	args := []string{"rm"}
	clusters := []string{}

	// operate on one or all clusters
	if !c.Bool("all") {
		clusters = append(clusters, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			log.Fatalf("ERROR: `--all` specified, but no clusters were found.")
		}
		clusters = append(clusters, clusterList...)
	}

	// remove clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Removing cluster [%s]", cluster)
		args = append(args, cluster)
		if err := runCommand(true, cmd, args...); err != nil {
			log.Printf("WARNING: couldn't delete cluster [%s], trying a force remove now.", cluster)
			args = args[:len(args)-1] // pop last element from list (name of cluster)
			args = append(args, "-f", cluster)
			if err := runCommand(true, cmd, args...); err != nil {
				log.Printf("FAILURE: couldn't delete cluster [%s] -> %+v", cluster, err)
			}
			args = args[:len(args)-1] // pop last element from list (-f flag)
		}
		deleteClusterDir(cluster)
		log.Printf("SUCCESS: removed cluster [%s]", cluster)
		args = args[:len(args)-1] // pop last element from list (name of last cluster)
	}
	return nil
}

// StopCluster stops a running cluster container (restartable)
func StopCluster(c *cli.Context) error {
	cmd := "docker"
	args := []string{"stop"}
	clusters := []string{}

	// operate on one or all clusters
	if !c.Bool("all") {
		clusters = append(clusters, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			log.Fatalf("ERROR: `--all` specified, but no clusters were found.")
		}
		clusters = append(clusters, clusterList...)
	}

	// stop clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Stopping cluster [%s]", cluster)
		args = append(args, cluster)
		if err := runCommand(true, cmd, args...); err != nil {
			log.Printf("FAILURE: couldn't stop cluster [%s] -> %+v", cluster, err)
		}
		log.Printf("SUCCESS: stopped cluster [%s]", cluster)
		args = args[:len(args)-1] // pop last element from list (name of last cluster)
	}
	return nil
}

// StartCluster starts a stopped cluster container
func StartCluster(c *cli.Context) error {
	cmd := "docker"
	args := []string{"start"}
	clusters := []string{}

	// operate on one or all clusters
	if !c.Bool("all") {
		clusters = append(clusters, c.String("name"))
	} else {
		clusterList, err := getClusterNames()
		if err != nil {
			log.Fatalf("ERROR: `--all` specified, but no clusters were found.")
		}
		clusters = append(clusters, clusterList...)
	}

	// start clusters one by one instead of appending all names to the docker command
	// this allows for more granular error handling and logging
	for _, cluster := range clusters {
		log.Printf("Starting cluster [%s]", cluster)
		args = append(args, cluster)
		if err := runCommand(true, cmd, args...); err != nil {
			log.Printf("FAILURE: couldn't start cluster [%s] -> %+v", cluster, err)
		}
		log.Printf("SUCCESS: started cluster [%s]", cluster)

		// pop last element from list (name of last cluster)
		args = args[:len(args)-1]
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

	if err := runCommand(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't get kubeconfig for cluster [%s] --> %+v", c.String("name"), err)
		return err
	}

	// Prints the path of the copied kubeconfig file (kubeconfig.yaml) on the local host.
	fmt.Printf("%s\n", path.Join(destPath, "Kubeconfig.yaml"))
	return nil
}