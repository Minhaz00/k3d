package main

import (
	"log"
	"os"

	"github.com/Minhaz00/k3d/version"
	"github.com/Minhaz00/k3d/cli"
	"github.com/urfave/cli"
)


func main() {

	// App details
	app := cli.NewApp()
	app.Name = "k3d"
	app.Usage = "Run k3s in Docker!"
	// app.Version = "v0.1.0"4
	app.Version = version.GetVersion()
	app.Authors = []cli.Author{
		cli.Author{
			Name:  "Minhaz",
			Email: "minhaz.jisun@gmail.com",
		},
	}

	// commands to execute
	app.Commands = []cli.Command{
		// check-tools or ct verifies that docker is up and running by by executing the 'docker version' command.
		{
			Name:    "check-tools",
			Aliases: []string{"ct"},
			Usage:   "Check if docker is running",
			Action: run.CheckTools,
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
					Usage: "Mount a volume into the cluster node (Docker notation: `source:destination`)",
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
				cli.IntFlag{
					Name:  "timeout, t",
					Value: 0,
					Usage: "Set the timeout value when --wait flag is set",
				},
				cli.BoolFlag{
					Name:  "wait, w",
					Usage: "Wait for the cluster to come up",
				},
			},
			Action: run.CreateCluster,
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
				cli.BoolFlag{
					Name:  "all, a",
					Usage: "delete all existing clusters (this ignores the --name/-n flag)",
				},
			},
			Action: run.DeleteCluster,
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
				cli.BoolFlag{
					Name:  "all, a",
					Usage: "stop all running clusters (this ignores the --name/-n flag)",
				},
			},
			Action: run.StopCluster,
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
				cli.BoolFlag{
					Name:  "all, a",
					Usage: "start all stopped clusters (this ignores the --name/-n flag)",
				},
			},
			Action: run.StartCluster,
		},

		// list prints a list of created clusters
		{
			Name:    "list",
			Aliases: []string{"ls", "l"},
			Usage:   "List all clusters",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "all, a",
					Usage: "Also show non-running clusters",
				},
			},
			Action: run.ListClusters,
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
				cli.BoolFlag{
					Name:  "all, a",
					Usage: "get kubeconfig for all clusters (this ignores the --name/-n flag)",
				},
			},
			Action: run.GetKubeConfig,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
