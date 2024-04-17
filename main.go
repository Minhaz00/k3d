package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/urfave/cli"
)

// Changes from nabil

func createCluster(c *cli.Context) error {
	port := fmt.Sprintf("%s:%s", c.String("port"), c.String("port"))
	// image := fmt.Sprintf("rancher/k3s:%s", c.String("version"))
	image := fmt.Sprintf("rancher/k3s:%s", c.String("version"))
	cmd := "docker"
	args := []string{
		"run",
		"--name", c.String("name"),
		"-e", "K3S_KUBECONFIG_OUTPUT=/output/kubeconfig.yaml",
		"--publish", port,
		"--privileged",
	}
	extraArgs := []string{}
	if c.IsSet("volume") {
		extraArgs = append(extraArgs, fmt.Sprintf("--volume %s", c.String("volume")))
	}
	if len(extraArgs) > 0 {
		for _, extra := range extraArgs {
			args = append(args, extra)
		}
	}
	args = append(args,
		"-d",
		image,
		"server",                                // cmd
		"--https-listen-port", c.String("port"), //args
	)
	log.Printf("Creating cluster [%s]", c.String("name"))
	log.Printf("Running command: %+v", exec.Command(cmd, args...).Args)
	if err := exec.Command(cmd, args...).Run(); err != nil {
		log.Fatalf("FAILURE: couldn't create cluster [%s] Err: %+v", c.String("name"), err)
		return err
	}
	log.Printf("SUCCESS: created cluster [%s]", c.String("name"))
	return nil
}

func deleteCluster(c *cli.Context) error {
	cmd := "docker"
	args := []string{"rm", "-f", c.String("name")}
	log.Printf("Deleting cluster [%s]", c.String("name"))
	log.Printf("Running command: %+v", exec.Command(cmd, args...).Args)
	if err := exec.Command(cmd, args...).Run(); err != nil {
		log.Fatalf("FAILURE: couldn't delete cluster [%s] Err: %+v", c.String("name"), err)
		return err
	}
	log.Printf("SUCCESS: deleted cluster [%s]", c.String("name"))
	return nil
}

func main() {

	var clusterName string
	var serverPort int
	var volume string
	var k3sVersion string

	app := cli.NewApp()
	app.Name = "k3d"
	app.Usage = "Run k3s in Docker!"
	app.Version = "0.0.1"
	app.Authors = []cli.Author{
		cli.Author{
			Name:  "Minhaz",
			Email: "minhaz.jisun@gmail.com",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:    "check-tools",
			Aliases: []string{"ct"},
			Usage:   "Check if docker is running",
			Action: func(c *cli.Context) error {
				log.Print("Checking docker...")
				cmd := "docker"
				args := []string{"version"}
				if err := exec.Command(cmd, args...).Run(); err != nil {
					log.Fatalf("Checking docker: FAILED")
					return err
				}
				log.Println("Checking docker: SUCCESS")
				return nil
			},
		},
		{
			Name:    "create",
			Aliases: []string{"c"},
			Usage:   "Create a single node k3s cluster in a container",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "name, n",
					Value:       "k3s_default",
					Usage:       "Set a name for the cluster",
					Destination: &clusterName,
				},
				cli.StringFlag{
					Name:        "volume, v",
					Usage:       "Mount a volume into the cluster node (Docker notation: `source:destination`",
					Destination: &volume,
				},
				cli.StringFlag{
					Name: "version",
					// Value:       "v0.1.0",
					Value:       "v1.29.4-rc1-k3s1",
					Usage:       "Choose the k3s image version",
					Destination: &k3sVersion,
				},
				cli.IntFlag{
					Name:        "port, p",
					Value:       6443,
					Usage:       "Set a port on which the ApiServer will listen",
					Destination: &serverPort,
				},
			},
			Action: createCluster,
		},

		{
			Name:  "stop",
			Usage: "Stop cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "name, n",
					Value:       "k3s_default",
					Usage:       "name of the cluster",
					Destination: &clusterName,
				},
			},
			Action: func(c *cli.Context) error {
				fmt.Println("Stopping cluster")
				return nil
			},
		},
		{
			Name:  "start",
			Usage: "Start a stopped cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "name, n",
					Value:       "k3s_default",
					Usage:       "name of the cluster",
					Destination: &clusterName,
				},
			},
			Action: func(c *cli.Context) error {
				fmt.Println("Starting stopped cluster")
				return nil
			},
		},
		{
			Name:  "list",
			Usage: "List all clusters",
			Action: func(c *cli.Context) error {
				fmt.Println("Listing clusters")
				return nil
			},
		},
		{
			Name:  "get-config",
			Usage: "Get kubeconfig location for cluster",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "name, n",
					Value:       "k3s_default",
					Usage:       "name of the cluster",
					Destination: &clusterName,
				},
			},
			Action: func(c *cli.Context) error {
				fmt.Println("Starting stopped cluster")
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
