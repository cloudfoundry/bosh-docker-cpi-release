package config_test

import (
	"encoding/json"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/config"
)

var _ = Describe("Config", func() {
	Describe("DockerOpts", func() {
		Describe("RequiresTLS", func() {
			It("returns false for unix socket hosts", func() {
				opts := config.DockerOpts{Host: "unix:///var/run/docker.sock"}
				Expect(opts.RequiresTLS()).To(BeFalse())
			})

			It("returns true for tcp hosts", func() {
				opts := config.DockerOpts{Host: "tcp://192.168.1.1:2376"}
				Expect(opts.RequiresTLS()).To(BeTrue())
			})

			It("returns true for https hosts", func() {
				opts := config.DockerOpts{Host: "https://192.168.1.1:2376"}
				Expect(opts.RequiresTLS()).To(BeTrue())
			})
		})

		Describe("Validate", func() {
			It("returns error when Host is empty", func() {
				opts := config.DockerOpts{APIVersion: "1.24"}
				Expect(opts.Validate()).To(MatchError(ContainSubstring("Must provide non-empty Host")))
			})

			It("returns error when APIVersion is empty", func() {
				opts := config.DockerOpts{Host: "unix:///var/run/docker.sock"}
				Expect(opts.Validate()).To(MatchError(ContainSubstring("Must provide non-empty APIVersion")))
			})

			It("succeeds for a unix socket host without TLS", func() {
				opts := config.DockerOpts{
					Host:       "unix:///var/run/docker.sock",
					APIVersion: "1.24",
				}
				Expect(opts.Validate()).To(Succeed())
			})

			Context("when TLS is required", func() {
				It("returns error when CA is empty", func() {
					opts := config.DockerOpts{
						Host:       "tcp://192.168.1.1:2376",
						APIVersion: "1.24",
						TLS:        config.DockerOptsTLS{Certificate: "cert", PrivateKey: "key"},
					}
					Expect(opts.Validate()).To(MatchError(ContainSubstring("Must provide non-empty CA")))
				})

				It("returns error when Certificate is empty", func() {
					opts := config.DockerOpts{
						Host:       "tcp://192.168.1.1:2376",
						APIVersion: "1.24",
						TLS:        config.DockerOptsTLS{CA: "ca", PrivateKey: "key"},
					}
					Expect(opts.Validate()).To(MatchError(ContainSubstring("Must provide non-empty Certificate")))
				})

				It("returns error when PrivateKey is empty", func() {
					opts := config.DockerOpts{
						Host:       "tcp://192.168.1.1:2376",
						APIVersion: "1.24",
						TLS:        config.DockerOptsTLS{CA: "ca", Certificate: "cert"},
					}
					Expect(opts.Validate()).To(MatchError(ContainSubstring("Must provide non-empty PrivateKey")))
				})

				It("succeeds when all TLS fields are provided", func() {
					opts := config.DockerOpts{
						Host:       "tcp://192.168.1.1:2376",
						APIVersion: "1.24",
						TLS:        config.DockerOptsTLS{CA: "ca", Certificate: "cert", PrivateKey: "key"},
					}
					Expect(opts.Validate()).To(Succeed())
				})
			})
		})
	})

	Describe("FactoryOpts", func() {
		Describe("Validate", func() {
			It("returns error when Docker configuration is invalid", func() {
				opts := config.FactoryOpts{
					Docker: config.DockerOpts{},
					Agent:  apiv1.AgentOptions{Mbus: "https://user:pass@127.0.0.1:4321/agent"},
				}
				Expect(opts.Validate()).To(MatchError(ContainSubstring("Validating Docker configuration")))
			})

			It("returns error when Agent configuration is invalid", func() {
				opts := config.FactoryOpts{
					Docker: config.DockerOpts{Host: "unix:///var/run/docker.sock", APIVersion: "1.24"},
					Agent:  apiv1.AgentOptions{},
				}
				Expect(opts.Validate()).To(MatchError(ContainSubstring("Validating Agent configuration")))
			})

			It("succeeds when both Docker and Agent are valid", func() {
				opts := config.FactoryOpts{
					Docker: config.DockerOpts{Host: "unix:///var/run/docker.sock", APIVersion: "1.24"},
					Agent:  apiv1.AgentOptions{Mbus: "https://user:pass@127.0.0.1:4321/agent"},
				}
				Expect(opts.Validate()).To(Succeed())
			})
		})
	})

	Describe("Config", func() {
		Describe("Validate", func() {
			It("returns error when Actions configuration is invalid", func() {
				cfg := config.Config{
					Actions: config.FactoryOpts{
						Docker: config.DockerOpts{},
					},
				}
				Expect(cfg.Validate()).To(MatchError(ContainSubstring("Validating Actions configuration")))
			})

			It("succeeds when Actions configuration is valid", func() {
				cfg := config.Config{
					Actions: config.FactoryOpts{
						Docker: config.DockerOpts{Host: "unix:///var/run/docker.sock", APIVersion: "1.24"},
						Agent:  apiv1.AgentOptions{Mbus: "https://user:pass@127.0.0.1:4321/agent"},
					},
				}
				Expect(cfg.Validate()).To(Succeed())
			})
		})

		Describe("JSON unmarshalling", func() {
			It("unmarshals all fields correctly", func() {
				data := `{
					"actions": {
						"docker": {
							"host": "unix:///var/run/docker.sock",
							"api_version": "1.24"
						},
						"agent": {
							"mbus": "https://user:pass@127.0.0.1:4321/agent"
						}
					},
					"start_containers_with_systemd": true,
					"enable_lxcfs_support": true,
					"light_stemcell": {
						"require_image_verification": true
					}
				}`

				var cfg config.Config
				err := json.Unmarshal([]byte(data), &cfg)
				Expect(err).NotTo(HaveOccurred())

				Expect(cfg.StartContainersWithSystemD).To(BeTrue())
				Expect(cfg.EnableLXCFSSupport).To(BeTrue())
				Expect(cfg.LightStemcell.RequireImageVerification).To(BeTrue())
				Expect(cfg.Actions.Docker.Host).To(Equal("unix:///var/run/docker.sock"))
				Expect(cfg.Actions.Docker.APIVersion).To(Equal("1.24"))
				Expect(cfg.Actions.Agent.Mbus).To(Equal("https://user:pass@127.0.0.1:4321/agent"))
			})

			It("unmarshals TLS private_key json tag correctly", func() {
				data := `{
					"host": "tcp://192.168.1.1:2376",
					"api_version": "1.24",
					"tls": {
						"ca": "ca-content",
						"certificate": "cert-content",
						"private_key": "key-content"
					}
				}`

				var opts config.DockerOpts
				err := json.Unmarshal([]byte(data), &opts)
				Expect(err).NotTo(HaveOccurred())
				Expect(opts.TLS.PrivateKey).To(Equal("key-content"))
			})
		})
	})
})
