package vm

import (
	"bosh-docker-cpi/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

func networksWithDNS(dns ...string) apiv1.Networks {
	return apiv1.Networks{"default": apiv1.NewNetwork(apiv1.NetworkOpts{DNS: dns})}
}

func TestResolveConfWritesOnlyValidatedIPs(t *testing.T) {
	command, err := populateResolveConf(networksWithDNS("192.0.2.53", "2001:0db8::53"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "resolv.conf")
	command = strings.ReplaceAll(command, "/etc/resolv.conf", path)
	if output, err := exec.Command("bash", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("%s: %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nameserver 192.0.2.53\nnameserver 2001:db8::53\n" {
		t.Fatalf("unexpected resolv.conf: %s", data)
	}
	command, err = populateResolveConf(nil)
	if err != nil || command != ":" {
		t.Fatalf("empty DNS: %q %v", command, err)
	}
}

func TestCreateRejectsInvalidDNSBeforeDockerOperations(t *testing.T) {
	for _, dns := range []string{"$(touch injected)", "8.8.8.8\"; echo injected; #", "`echo injected`", "192.0.2.1\nnameserver injected", "example.com", ""} {
		t.Run(dns, func(t *testing.T) {
			props := emptyVMProps{}
			// A nil Docker client deliberately makes any API call fail: invalid DNS must
			// be rejected before creating networks, volumes, or containers.
			_, err := (Factory{Config: config.Config{}}).Create(apiv1.NewAgentID("agent"), nil, props, networksWithDNS(dns), nil, apiv1.VMEnv{})
			if err == nil || !strings.Contains(err.Error(), "DNS server must be an IP address") {
				t.Fatalf("DNS %q: %v", dns, err)
			}
		})
	}
}

type emptyVMProps struct{ apiv1.VMCloudProps }

func (emptyVMProps) As(interface{}) error { return nil }
