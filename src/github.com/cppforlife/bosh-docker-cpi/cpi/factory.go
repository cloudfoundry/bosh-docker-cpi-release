package cpi

import (
	"crypto/tls"
	"net/http"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
	dkrclient "github.com/docker/engine-api/client"
	dkrtlsconfig "github.com/docker/go-connections/tlsconfig"

	bdisk "github.com/cppforlife/bosh-docker-cpi/disk"
	bstem "github.com/cppforlife/bosh-docker-cpi/stemcell"
	bvm "github.com/cppforlife/bosh-docker-cpi/vm"
)

type Factory struct {
	fs      boshsys.FileSystem
	uuidGen boshuuid.Generator
	opts    FactoryOpts
	logger  boshlog.Logger
}

type CPI struct {
	InfoMethod

	CreateStemcellMethod
	DeleteStemcellMethod

	CreateVMMethod
	DeleteVMMethod
	HasVMMethod
	RebootVMMethod
	SetVMMetadataMethod
	GetDisksMethod

	CreateDiskMethod
	DeleteDiskMethod
	AttachDiskMethod
	DetachDiskMethod
	HasDiskMethod
}

func NewFactory(
	fs boshsys.FileSystem,
	uuidGen boshuuid.Generator,
	opts FactoryOpts,
	logger boshlog.Logger,
) Factory {
	return Factory{fs, uuidGen, opts, logger}
}

func (f Factory) New(ctx apiv1.CallContext) (apiv1.CPI, error) {
	opts, err := f.dockerOpts(ctx, f.opts.Docker)
	if err != nil {
		return CPI{}, err
	}

	httpClient, err := f.httpClient(opts)
	if err != nil {
		return CPI{}, err
	}

	dkrClient, err := dkrclient.NewClient(opts.Host, opts.APIVersion, httpClient, nil)
	if err != nil {
		return CPI{}, err
	}

	stemcellImporter := bstem.NewFSImporter(dkrClient, f.fs, f.uuidGen, f.logger)
	stemcellFinder := bstem.NewFSFinder(dkrClient, f.logger)
	vmFactory := bvm.NewFactory(dkrClient, f.uuidGen, f.opts.Agent, f.logger)
	diskFactory := bdisk.NewFactory(dkrClient, f.uuidGen, f.logger)

	return CPI{
		NewInfoMethod(),

		NewCreateStemcellMethod(stemcellImporter),
		NewDeleteStemcellMethod(stemcellFinder),

		NewCreateVMMethod(stemcellFinder, vmFactory),
		NewDeleteVMMethod(vmFactory),
		NewHasVMMethod(vmFactory),
		NewRebootVMMethod(),
		NewSetVMMetadataMethod(),
		NewGetDisksMethod(vmFactory),

		NewCreateDiskMethod(diskFactory),
		NewDeleteDiskMethod(diskFactory),
		NewAttachDiskMethod(vmFactory, diskFactory),
		NewDetachDiskMethod(vmFactory, diskFactory),
		NewHasDiskMethod(diskFactory),
	}, nil
}

func (Factory) dockerOpts(ctx apiv1.CallContext, defaults DockerOpts) (DockerOpts, error) {
	var opts DockerOpts

	err := ctx.As(&opts)
	if err != nil {
		return DockerOpts{}, bosherr.WrapError(err, "Parsing CPI context")
	}

	if len(opts.Host) > 0 {
		err := opts.Validate()
		if err != nil {
			return DockerOpts{}, bosherr.WrapError(err, "Validating CPI context")
		}
	} else {
		opts = defaults
	}

	return opts, nil
}

func (Factory) httpClient(opts DockerOpts) (*http.Client, error) {
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
