// Copyright © 2016 Apigee Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/30x/argonaut/utils"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var containerFlag string
var tailFlag int
var followFlag bool

// logsCmd represents the logs command
var logsCmd = &cobra.Command{
	Use:   "logs <labelSelector>",
	Short: "Print the logs for a container in all matching pods.",
	Long: `Print the logs for a container in all matching pods. If the pod has only one container, the container name is optional.
Examples:
# Return snapshot logs in all "app=hello" pods with only one container
argonaut logs "app=hello"

# Return snapshot logs in the ingress container for all "app=hello" pods
argonaut logs "app=hello" -c ingress`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			fmt.Println("Missing required argument: labelSelector")
			return
		}

		labelSelector := args[0]

		fmt.Printf("\nRetrieving logs...this could take a minute.\n\n")

		// retrieve k8s client via .kube/config
		client, err := utils.GetClient()
		if err != nil {
			fmt.Println(err)
			return
		}

		err = GetMultiLogs(client, labelSelector, namespaceFlag, containerFlag, tailFlag, followFlag, colorFlag)
		if err != nil {
			fmt.Println(err)
		}

		return
	},
}

// GetMultiLogs retrieves all logs for the given label selector
func GetMultiLogs(client *kubernetes.Clientset, labelSelector string, namespace string, container string, tail int, follow bool, useColor bool) error {
	// parse given label selector
	selector, err := labels.Parse(labelSelector)
	if err != nil {
		return err
	}

	// determine namespace to query
	if namespace == "" {
		namespace = api.NamespaceDefault
	}

	podIntr := client.Pods(namespace)

	// retrieve all pods by label selector
	pods, err := podIntr.List(metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: selector.String(),
	})
	if err != nil {
		return err
	}

	// notify caller that there were no pods
	if len(pods.Items) == 0 {
		return fmt.Errorf("No pods in namespace: " + namespace)
	}

	var wg sync.WaitGroup
	var col *color.Color
	colorLen := len(colors)

	// iterate over pods and get logs
	for ndx, pod := range pods.Items {
		// set pod logging options
		podLogOpts := &api.PodLogOptions{}
		if container != "" {
			podLogOpts.Container = container
		}

		// set tail line count
		if tail != -1 {
			convTail := int64(tail)
			podLogOpts.TailLines = &convTail
		}

		// defaults to false
		podLogOpts.Follow = follow

		if useColor {
			col = colors[ndx%colorLen] // give this stream one of the set colors
		} else {
			color.NoColor = true           // turn off all colors
			col = color.New(color.FgWhite) // set color to white to be safe
		}

		// get specified pod's log request and run it
		req := podIntr.GetLogs(pod.Name, podLogOpts)
		stream, err := req.Stream()
		if err != nil {
			return err
		}

		// attach to and stream logs for this container until stopped
		if follow {
			wg.Add(1)
			go openLogStream(stream, pod.Name, &wg, col)
		} else { // gather log request output and dump to stdout
			col.Println("Logs for pod", pod.Name, ":")

			defer stream.Close()
			_, err = io.Copy(os.Stdout, stream)
			if err != nil {
				return err
			}
		}
	}

	if follow {
		wg.Wait()
	}

	return nil
}

func openLogStream(stream io.ReadCloser, podName string, wg *sync.WaitGroup, col *color.Color) {
	defer stream.Close()
	defer wg.Done()

	buf := bufio.NewReader(stream)
	for {
		line, _, err := buf.ReadLine()
		if err != nil {
			fmt.Println("Error from routine for", podName, ":", err)
			return
		}

		col.Printf("POD %s: ", podName)
		fmt.Printf("%q\n", line)
	}
}

func init() {
	RootCmd.AddCommand(logsCmd)
	logsCmd.Flags().StringVarP(&containerFlag, "container", "c", "", "Print the logs of this container")
	logsCmd.Flags().IntVarP(&tailFlag, "tail", "t", -1, "Lines of recent log file to display. Defaults to -1, showing all log lines.")
	logsCmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "Specify if the logs should be streamed.")
}
