package mesh

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/layer5io/meshery/mesheryctl/internal/cli/root/config"
	"github.com/layer5io/meshery/mesheryctl/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// checkArgs checks whether the user has supplied an adapter(-a) argument
func checkArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n || args[0] == "" {
			return errors.New(utils.MeshError("'--adapter' (Adapter to use for installation) argument is required in the 'mesheryctl mesh deploy' command.\n"))
		}
		return nil
	}
}

var (
	meshName  string
	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a service mesh to the Kubernetes cluster",
		Args:  checkArgs(1),
		Long:  `Deploy a service mesh to the connected Kubernetes cluster`,
		Example: `
// Deploy a service mesh from an interactive on the default namespace
mesheryctl mesh deploy

// Deploy Linkerd in a specific namespace:
mesheryctl mesh deploy linkerd --namespace linkerd-ns

// Deploy Linkerd and wait for it to be deployed:
mesheryctl mesh deploy linkerd --watch`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Infof(fmt.Sprintf("Deploying %s...", meshName))
		_, err = sendOperationRequest(mctlCfg, strings.ToLower(meshName), false)
		if err != nil {
			log.Fatalln(err)
		}

		if watch {
			_, err = waitForDeployResponse(mctlCfg, "mesh is now installed")
			if err != nil {
				log.Fatalln(err)
			}
		}

		return nil
	},
}

func init() {
	deployCmd.Flags().StringVarP(
		&namespace, "namespace", "n", "default",
		"Kubernetes namespace where the mesh will be deployed",
	)
	deployCmd.Flags().BoolVarP(
		&watch, "watch", "w", false,
		"Watch for events and verify operation (in beta testing)",
	)
}

func waitForDeployResponse(mctlCfg *config.MesheryCtlConfig, query string) (string, error) {
	path := mctlCfg.GetBaseMesheryURL() + "/api/events?client=cli_deploy"
	method := "GET"
	client := &http.Client{}
	req, err := utils.NewRequest(method, path, nil)
	if err != nil {
		return "", ErrCreatingDeployRequest(err)
	}

	res, err := client.Do(req)
	if err != nil {
		return "", ErrCreatingDeployResponseRequest(err)
	}
	defer res.Body.Close()

	event, err := utils.ConvertRespToSSE(res)
	if err != nil {
		return "", ErrCreatingDeployResponseStream(err)
	}

	timer := time.NewTimer(time.Duration(1200) * time.Second)
	eventChan := make(chan string)

	//Run a goroutine to wait for the response
	go func() {
		for i := range event {
			if strings.Contains(i.Data.Details, query) {
				eventChan <- "successful"
				log.Infof("%s\n%s\n", i.Data.Summary, i.Data.Details)
			} else if strings.Contains(i.Data.Details, "Error") {
				eventChan <- "error"
				log.Infof("%s\n", i.Data.Summary)
			}
		}
	}()

	select {
	case <-timer.C:
		return "", ErrTimeoutWaitingForDeployResponse
	case event := <-eventChan:
		if event != "successful" {
			return "", ErrFailedDeployingMesh
		}
	}

	return "", nil
}
