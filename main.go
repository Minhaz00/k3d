package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"

	"github.com/urfave/cli"
)

// Command: [docker run --name k3s_default -e K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml --publish 6443:6443 --privileged -d rancher/k3s:v1.29.4-rc1-k3s1 server --https-listen-port 6443]
func createCluster(c *cli.Context) error {

	createClusterDir(c.String("name"))

	port := fmt.Sprintf("%s:%s", c.String("port"), c.String("port"))
	image := fmt.Sprintf("rancher/k3s:%s", c.String("version"))
	cmd := "docker"

	//required arguments
	args := []string{
		"run",
		"--name", c.String("name"),
		"-e", "K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml",
		"--publish", port,
		"--privileged",
	}

	//list of extra argument
	extraArgs := []string{}

	//append the extra argument --volume if specified
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
	log.Printf("Creating cluster [%s]", c.String("name"))
	if err := run(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't create cluster [%s] --> %+v", c.String("name"), err)
		return err
	}
	log.Printf("SUCCESS: created cluster [%s]", c.String("name"))

	// print a simple command line to the screen to help the user along
	log.Printf(`You can now use the cluster with: 
	export KUBECONFIG="$(%s get-kubeconfig --name='%s')" 
	kubectl cluster-info`, os.Args[0], c.String("name"))
	return nil
}

// Command: docker rm -f Cluster_name
func deleteCluster(c *cli.Context) error {
	cmd := "docker"
	args := []string{"rm", c.String("name")}

	log.Printf("Deleting cluster [%s]", c.String("name"))

	if err := run(true, cmd, args...); err != nil {
		log.Printf("WARNING: couldn't delete cluster [%s], trying a force remove now.", c.String("name"))

		//adding -f flag to delete the cluster forcefully
		args = append(args, "-f")
		if err := run(true, cmd, args...); err != nil {
			log.Fatalf("FAILURE: couldn't delete cluster [%s] --> %+v", c.String("name"), err)
			return err
		}
	}
	deleteClusterDir(c.String("name"))
	log.Printf("SUCCESS: deleted cluster [%s]", c.String("name"))
	return nil
}

// Command: docker stop Cluster_name
func stopCluster(c *cli.Context) error {

	cmd := "docker"
	args := []string{"stop", c.String("name")}

	log.Printf("Stopping cluster [%s]", c.String("name"))

	if err := run(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't stop cluster [%s] --> %+v", c.String("name"), err)
		return err
	}

	log.Printf("SUCCESS: stopped cluster [%s]", c.String("name"))
	return nil
}

// Command: docker start Cluster_name
func startCluster(c *cli.Context) error {

	cmd := "docker"
	args := []string{"start", c.String("name")}

	log.Printf("Starting cluster [%s]", c.String("name"))

	if err := run(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't start cluster [%s] --> %+v", c.String("name"), err)
		return err
	}

	log.Printf("SUCCESS: started cluster [%s]", c.String("name"))
	return nil
}

// listing all the directories of all clusters
func listClusters(c *cli.Context) error {
	printClusters(c.Bool("all"))
	return nil
}

// getKubeConfig grabs the kubeconfig from the running cluster and prints the path to stdout
func getKubeConfig(c *cli.Context) error {
	// Construct the source path within the Docker container where the kubeconfig file is located
	sourcePath := fmt.Sprintf("%s:/output/kubeconfig.yaml", c.String("name"))

	// Get the destination directory on the local host where the kubeconfig file will be copied
	destPath, _ := getClusterDir(c.String("name"))

	cmd := "docker"
	args := []string{"cp", sourcePath, destPath}

	if err := run(true, cmd, args...); err != nil {
		log.Fatalf("FAILURE: couldn't get kubeconfig for cluster [%s] --> %+v", c.String("name"), err)
		return err
	}

	// Prints the path of the copied kubeconfig file (kubeconfig.yaml) on the local host.
	fmt.Printf("%s\n", path.Join(destPath, "Kubeconfig.yaml"))
	return nil
}

func main() {

	// App details
	app := cli.NewApp()
	app.Name = "k3d"
	app.Usage = "Run k3s in Docker!"
	app.Version = "v0.0.2"
	app.Authors = []cli.Author{
		cli.Author{
			Name:  "Minhaz",
			Email: "minhaz.jisun@gmail.com",
		},
	}

	// commands to execute
	app.Commands = []cli.Command{
		// check-tools verifies that docker is up and running
		{
			Name:    "check-tools",
			Aliases: []string{"ct"},
			Usage:   "Check if docker is running",
			Action: func(c *cli.Context) error {
				log.Print("Checking docker...")
				cmd := "docker"
				args := []string{"version"}
				if err := run(true, cmd, args...); err != nil {
					log.Fatalf("Checking docker: FAILED")
					return err
				}
				log.Println("Checking docker: SUCCESS")
				return nil
			},
		},

		// create creates a new k3s cluster in a container
		{
			Name:    "create",
			Aliases: []string{"c"},
			Usage:   "Create a single node k3s cluster in a container",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "name, n",
					Value: "k3s_default",
					Usage: "Set a name for the cluster",
				},
				cli.StringFlag{
					Name:  "volume, v",
					Usage: "Mount a volume into the cluster node (Docker notation: `source:destination`",
				},
				cli.StringFlag{
					Name:  "version",
					Value: "v1.29.4-rc1-k3s1",
					Usage: "Choose the k3s image version",
				},
				cli.IntFlag{
					Name:  "port, p",
					Value: 6443,
					Usage: "Set a port on which the ApiServer will listen",
				},
			},
			Action: createCluster,
		},

		// delete deletes an existing k3s cluster (remove container and cluster directory)
		{
			Name:    "delete",
			Aliases: []string{"d"},
			Usage:   "Delete cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "name, n",
					Value: "k3s_default",
					Usage: "name of the cluster",
				},
			},
			Action: deleteCluster,
		},

		// stop stops a running cluster (its container) so it's restartable
		{
			Name:  "stop",
			Usage: "Stop cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "name, n",
					Value: "k3s_default",
					Usage: "name of the cluster",
				},
			},
			Action: stopCluster,
		},

		// start restarts a stopped cluster container
		{
			Name:  "start",
			Usage: "Start a stopped cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "name, n",
					Value: "k3s_default",
					Usage: "name of the cluster",
				},
			},
			Action: startCluster,
		},

		// list prints a list of created clusters
		{
			Name: "list",
			Usage: "List all clusters",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name: "all, a",
					Usage: "Also show non-running clusters",
				},
			},
			Action: listClusters,
		},

		// get-kubeconfig grabs the kubeconfig from the cluster and prints the path to it
		{
			Name:  "get-kubeconfig",
			Usage: "Get kubeconfig location for cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "name, n",
					Value: "k3s_default",
					Usage: "name of the cluster",
				},
			},
			Action: getKubeConfig,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func run(verbose bool, name string, args ...string) error {
	if verbose {
		log.Printf("Running command: %+v", append([]string{name}, args...))
	}
	// Create the command with the given arguments
	cmd := exec.Command(name, args...)
	// Set the command's output to be piped to the standard output
	cmd.Stdout = os.Stdout
	// Set the command's error output to be piped to the standard error
	cmd.Stderr = os.Stderr
	// Run the command
	return cmd.Run()
}
