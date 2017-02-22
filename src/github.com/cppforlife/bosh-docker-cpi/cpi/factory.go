package cpi

import (
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

func (f Factory) New(_ apiv1.CallContext) (apiv1.CPI, error) {
	host := f.opts.Docker.Host
	version := f.opts.Docker.APIVersion

	httpClient, err := f.httpClient()
	if err != nil {
		return CPI{}, err
	}

	dkrClient, err := dkrclient.NewClient(host, version, httpClient, nil)
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

func (f Factory) httpClient() (*http.Client, error) {
	certPool, err := dkrtlsconfig.SystemCertPool()
	if err != nil {
		return nil, bosherr.WrapError(err, "Adding system CA certs")
	}

	if !certPool.AppendCertsFromPEM([]byte(f.opts.Docker.CACert)) {
		return nil, bosherr.WrapError(err, "Appending configured CA certs")
	}

	tlsConfig := dkrtlsconfig.ClientDefault()
	tlsConfig.InsecureSkipVerify = false
	tlsConfig.RootCAs = certPool

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, nil
}
