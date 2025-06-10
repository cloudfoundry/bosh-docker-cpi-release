package cpi

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	dkrclient "github.com/docker/docker/client"
	dkrtlsconfig "github.com/docker/go-connections/tlsconfig"

	"bosh-docker-cpi/config"
	bdisk "bosh-docker-cpi/disk"
	bstem "bosh-docker-cpi/stemcell"
	bvm "bosh-docker-cpi/vm"
)

// Factory creates CPI instances with configured dependencies
type Factory struct {
	fs      boshsys.FileSystem
	uuidGen boshuuid.Generator
	opts    config.FactoryOpts
	logger  boshlog.Logger
	Config  config.Config
}

// CPI implements the BOSH Cloud Provider Interface for Docker
type CPI struct {
	InfoMethod

	CreateStemcellMethod
	DeleteStemcellMethod

	CreateVMMethod
	DeleteVMMethod
	CalculateVMCloudPropertiesMethod
	HasVMMethod
	RebootVMMethod
	SetVMMetadataMethod
	GetDisksMethod

	CreateDiskMethod
	DeleteDiskMethod
	AttachDiskMethod
	DetachDiskMethod
	HasDiskMethod

	Disks
	Snapshots
}

// NewFactory creates a new Factory with the given dependencies
func NewFactory(
	fs boshsys.FileSystem,
	uuidGen boshuuid.Generator,
	opts config.FactoryOpts,
	logger boshlog.Logger,
	cfg config.Config,
) Factory {
	return Factory{fs, uuidGen, opts, logger, cfg}
}

// New creates a new CPI instance with the given call context
func (f Factory) New(ctx apiv1.CallContext) (apiv1.CPI, error) {
	opts, err := f.dockerOpts(ctx, f.opts.Docker)
	if err != nil {
		return CPI{}, err
	}

	httpClient, err := f.httpClient(opts)
	if err != nil {
		return CPI{}, err
	}

	// Try to create Docker client with fallback mechanism
	dkrClient, err := f.createDockerClientWithFallback(opts, httpClient)
	if err != nil {
		return CPI{}, err
	}

	stemcellImporter := bstem.NewFSImporter(dkrClient, f.fs, f.uuidGen, f.logger)
	stemcellFinder := bstem.NewFSFinder(dkrClient, f.logger)
	vmFactory := bvm.NewFactory(dkrClient, f.uuidGen, f.opts.Agent, f.logger, f.Config)
	diskFactory := bdisk.NewFactory(dkrClient, f.uuidGen, f.logger)

	return CPI{
		NewInfoMethod(),

		NewCreateStemcellMethod(stemcellImporter),
		NewDeleteStemcellMethod(stemcellFinder),

		NewCreateVMMethod(stemcellFinder, vmFactory),
		NewDeleteVMMethod(vmFactory),
		NewCalculateVMCloudPropertiesMethod(),
		NewHasVMMethod(vmFactory),
		NewRebootVMMethod(),
		NewSetVMMetadataMethod(),
		NewGetDisksMethod(vmFactory),

		NewCreateDiskMethod(diskFactory),
		NewDeleteDiskMethod(diskFactory),
		NewAttachDiskMethod(vmFactory, diskFactory),
		NewDetachDiskMethod(vmFactory, diskFactory),
		NewHasDiskMethod(diskFactory),
		NewDisks(),
		NewSnapshots(),
	}, nil
}

func (Factory) dockerOpts(ctx apiv1.CallContext, defaults config.DockerOpts) (config.DockerOpts, error) {
	var opts config.DockerOpts

	err := ctx.As(&opts)
	if err != nil {
		return config.DockerOpts{}, bosherr.WrapError(err, "Parsing CPI context")
	}

	if len(opts.Host) > 0 {
		err := opts.Validate()
		if err != nil {
			return config.DockerOpts{}, bosherr.WrapError(err, "Validating CPI context")
		}
	} else {
		opts = defaults
	}

	return opts, nil
}

func (Factory) httpClient(opts config.DockerOpts) (*http.Client, error) {
	if !opts.RequiresTLS() {
		return nil, nil
	}

	certPool, err := dkrtlsconfig.SystemCertPool()
	if err != nil {
		return nil, bosherr.WrapError(err, "Adding system CA certs")
	}

	if !certPool.AppendCertsFromPEM([]byte(opts.TLS.CA)) {
		return nil, bosherr.WrapError(err, "Appending configured CA certs")
	}

	tlsConfig := dkrtlsconfig.ClientDefault()
	tlsConfig.InsecureSkipVerify = false
	tlsConfig.RootCAs = certPool

	tlsCert, err := tls.X509KeyPair([]byte(opts.TLS.Certificate), []byte(opts.TLS.PrivateKey))
	if err != nil {
		return nil, bosherr.WrapError(err, "Loading X509 key pair (make sure the key is not encrypted)")
	}

	tlsConfig.Certificates = []tls.Certificate{tlsCert}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, nil
}

func (f Factory) createDockerClientWithFallback(opts config.DockerOpts, httpClient *http.Client) (*dkrclient.Client, error) {
	f.logger.Debug("Factory", "Attempting to create Docker client with host: %s", opts.Host)
	// First, try with the configured host
	dkrClient, err := dkrclient.NewClientWithOpts(
		dkrclient.WithHost(opts.Host),
		dkrclient.WithVersion(opts.APIVersion),
		dkrclient.WithHTTPClient(httpClient),
	)
	if err == nil {
		// Test the connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, pingErr := dkrClient.Ping(ctx)
		if pingErr == nil {
			f.logger.Debug("Factory", "Successfully connected to Docker at %s", opts.Host)
			return dkrClient, nil
		}
		f.logger.Debug("Factory", "Failed to ping Docker at %s: %s", opts.Host, pingErr)
	}

	// If the original host is a unix socket and failed, try alternative socket paths
	if strings.HasPrefix(opts.Host, "unix://") {
		socketPaths := []string{
			opts.Host,                              // Original path
			"unix:///var/run/docker.sock",          // Standard Docker socket
			"unix:///docker.sock",                  // Alternative path used in some container setups
			"unix:///var/vcap/sys/run/docker.sock", // BOSH-specific path
		}

		// Also check if DOCKER_HOST env var is set
		if envHost := os.Getenv("DOCKER_HOST"); envHost != "" && !contains(socketPaths, envHost) {
			socketPaths = append(socketPaths, envHost)
		}

		for _, socketPath := range socketPaths {
			if socketPath == opts.Host {
				continue // Skip the original path we already tried
			}

			f.logger.Debug("Factory", "Trying alternative Docker socket: %s", socketPath)

			// For unix sockets, we don't use TLS
			var altHTTPClient *http.Client
			if strings.HasPrefix(socketPath, "unix://") {
				altHTTPClient = nil
			} else {
				altHTTPClient = httpClient
			}

			altClient, altErr := dkrclient.NewClientWithOpts(
				dkrclient.WithHost(socketPath),
				dkrclient.WithVersion(opts.APIVersion),
				dkrclient.WithHTTPClient(altHTTPClient),
			)
			if altErr != nil {
				f.logger.Debug("Factory", "Failed to create client for %s: %s", socketPath, altErr)
				continue
			}

			// Test the connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, pingErr := altClient.Ping(ctx)
			if pingErr == nil {
				f.logger.Info("Factory", "Successfully connected to Docker at %s (fallback from %s)", socketPath, opts.Host)
				return altClient, nil
			}
			f.logger.Debug("Factory", "Failed to ping Docker at %s: %s", socketPath, pingErr)
		}
	}

	// If we're using TCP and it failed, check if we should try unix socket as fallback
	if strings.HasPrefix(opts.Host, "tcp://") || strings.HasPrefix(opts.Host, "http://") {
		// Try common unix socket paths as fallback
		unixPaths := []string{
			"unix:///var/run/docker.sock",
			"unix:///docker.sock",
			"unix:///var/vcap/sys/run/docker.sock",
		}

		for _, socketPath := range unixPaths {
			f.logger.Debug("Factory", "Trying unix socket fallback: %s", socketPath)

			altClient, altErr := dkrclient.NewClientWithOpts(
				dkrclient.WithHost(socketPath),
				dkrclient.WithVersion(opts.APIVersion),
				dkrclient.WithHTTPClient(nil), // No TLS for unix sockets
			)
			if altErr != nil {
				continue
			}

			// Test the connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, pingErr := altClient.Ping(ctx)
			if pingErr == nil {
				f.logger.Info("Factory", "Successfully connected to Docker at %s (fallback from %s)", socketPath, opts.Host)
				return altClient, nil
			}
		}
	}

	// All attempts failed, return the original error
	return nil, bosherr.WrapErrorf(err, "Failed to connect to Docker at %s and all fallback attempts failed", opts.Host)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
