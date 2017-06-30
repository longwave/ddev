package servicetest_test

import (
	"os"
	"strings"
	"testing"

	"path/filepath"

	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/drud/ddev/pkg/dockerutil"
	"github.com/drud/ddev/pkg/fileutil"
	"github.com/drud/ddev/pkg/plugins/platform"
	"github.com/drud/ddev/pkg/testcommon"
	"github.com/drud/ddev/pkg/util"
	"github.com/stretchr/testify/assert"
)

var (
	TestSites = []testcommon.TestSite{
		{
			Name:                          "TestServicesDrupal8",
			SourceURL:                     "https://github.com/drud/drupal8/archive/v0.6.0.tar.gz",
			ArchiveInternalExtractionPath: "drupal8-0.6.0/",
		},
	}
	ServiceFiles []string
	ServiceDir   string
)

func TestServicesSetup(t *testing.T) {
	if !testing.Short() {
		var err error
		assert := assert.New(t)

		ServiceDir, err = filepath.Abs("../../services")
		assert.NoError(err)

		err = filepath.Walk(ServiceDir, func(path string, f os.FileInfo, _ error) error {
			if !f.IsDir() && strings.HasPrefix(f.Name(), "docker-compose") {
				ServiceFiles = append(ServiceFiles, f.Name())
			}
			return nil
		})
		assert.NoError(err)

		err = dockerutil.EnsureNetwork(dockerutil.GetDockerClient(), dockerutil.NetName)
		assert.NoError(err)
	} else {
		log.Info("services tests skipped in short mode")
	}
}

// TestServices tests each service compose file in the services folder.
// It tests that a site can fully start w/ the compose file present, and
// checks that any exposed HTTP ports return 200.
func TestServices(t *testing.T) {
	assert := assert.New(t)

	if len(ServiceFiles) > 0 {
		for _, site := range TestSites {
			err := site.Prepare()
			if err != nil {
				log.Fatalf("Prepare() failed on TestSite.Prepare(), err=%v", err)
			}

			app, err := platform.GetPluginApp("local")
			assert.NoError(err)

			err = app.Init(site.Dir)
			assert.NoError(err)

			for _, service := range ServiceFiles {
				confdir := filepath.Join(app.AppRoot(), ".ddev")
				err = fileutil.CopyFile(filepath.Join(ServiceDir, service), filepath.Join(confdir, service))
				assert.NoError(err)
			}

			err = app.Start()
			assert.NoError(err)

			for _, service := range ServiceFiles {
				log.Info("Checking containers for ", service)
				serviceName := strings.TrimPrefix(service, "docker-compose.")
				serviceName = strings.TrimSuffix(serviceName, ".yaml")

				labels := map[string]string{
					"com.ddev.site-name":         app.GetName(),
					"com.docker.compose.service": serviceName,
				}

				container, err := dockerutil.FindContainerByLabels(labels)
				assert.NoError(err)
				if err != nil {
					t.Fatalf("Could not find running container for service %s. Skipping remainder of test.", serviceName)
				}
				name := dockerutil.ContainerName(container)
				check, err := testcommon.ContainerCheck(name, "running")
				assert.NoError(err)
				assert.True(check, serviceName, "container is running")

				// check container env for HTTP_EXPOSE ports to check
				expose := dockerutil.GetContainerEnv("HTTP_EXPOSE", container)
				if expose != "" {
					if strings.Contains(expose, ":") {
						ports := strings.Split(expose, ":")
						expose = ports[1]
					}

					containerPorts := container.Ports
					for _, port := range containerPorts {
						if string(port.PrivatePort) == expose && port.PublicPort != 0 {
							fmt.Println("Checking for 200 status for port ", port.PrivatePort)
							o := util.NewHTTPOptions("http://127.0.0.1:" + string(port.PublicPort))
							o.ExpectedStatus = 200
							o.Timeout = 30
							err = util.EnsureHTTPStatus(o)
							assert.NoError(err)
						}
					}
				}

			}

			err = app.Down()
			assert.NoError(err)
			site.Cleanup()
		}
	}
}
