package stemcell_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"

	bstem "bosh-docker-cpi/stemcell"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
)

var _ = Describe("Metadata", func() {
	Describe("Metadata structure", func() {
		It("should have all required fields for light stemcells", func() {
			metadata := bstem.Metadata{
				Name:            "test-stemcell",
				Version:         "1.0",
				StemcellFormats: []string{"docker-light"},
				CloudProperties: bstem.CloudProperties{
					ImageReference: "ghcr.io/test/stemcell:1.0",
					Digest:         "sha256:abc123",
				},
			}

			Expect(metadata.Name).To(Equal("test-stemcell"))
			Expect(metadata.Version).To(Equal("1.0"))
			Expect(metadata.IsLightStemcell()).To(BeTrue())
			Expect(metadata.GetImageReference()).To(Equal("ghcr.io/test/stemcell:1.0"))
			Expect(metadata.GetDigest()).To(Equal("sha256:abc123"))
		})

		It("should support metadata without digest", func() {
			metadata := bstem.Metadata{
				Name:            "test-stemcell",
				Version:         "1.0",
				StemcellFormats: []string{"docker-light"},
				CloudProperties: bstem.CloudProperties{
					ImageReference: "ghcr.io/test/stemcell:1.0",
				},
			}

			Expect(metadata.GetDigest()).To(Equal(""))
			Expect(metadata.IsLightStemcell()).To(BeTrue())
		})

		It("should support traditional stemcell metadata", func() {
			metadata := bstem.Metadata{
				Name:    "traditional-stemcell",
				Version: "2.0",
			}

			Expect(metadata.IsLightStemcell()).To(BeFalse())
			Expect(metadata.GetImageReference()).To(Equal(""))
		})
	})

	Describe("MetadataParser", func() {
		var (
			tempDir string
			logger  boshlog.Logger
			realFs  boshsys.FileSystem
		)

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "stemcell-test")
			Expect(err).NotTo(HaveOccurred())

			logger = boshlog.NewLogger(boshlog.LevelNone)
			realFs = boshsys.NewOsFileSystem(logger)
		})

		AfterEach(func() {
			err := os.RemoveAll(tempDir)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("ParseFromPath", func() {
			It("should parse a valid light stemcell with new format", func() {
				// Create a test light stemcell archive with new format
				stemcellPath := filepath.Join(tempDir, "light-stemcell-new.tgz")
				metadataContent := `name: ubuntu-noble
version: "1.165"
stemcell_formats:
  - docker-light
cloud_properties:
  image_reference: ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165
  digest: sha256:d4ca21a75f1ff6be382695e299257f054585143bf09762647bcb32f37be5eaf3
`
				createTestArchive(stemcellPath, "stemcell.MF", metadataContent)

				// Use real filesystem for this test since we need actual archive reading
				realParser := bstem.NewMetadataParser(realFs)
				metadata, isLight, err := realParser.ParseFromPath(stemcellPath)

				Expect(err).NotTo(HaveOccurred())
				Expect(isLight).To(BeTrue())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.Name).To(Equal("ubuntu-noble"))
				Expect(metadata.Version).To(Equal("1.165"))
				Expect(metadata.IsLightStemcell()).To(BeTrue())
				Expect(metadata.GetImageReference()).To(Equal("ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165"))
				Expect(metadata.GetDigest()).To(Equal("sha256:d4ca21a75f1ff6be382695e299257f054585143bf09762647bcb32f37be5eaf3"))
			})

			It("should parse a light stemcell without digest", func() {
				stemcellPath := filepath.Join(tempDir, "light-stemcell-no-digest.tgz")
				metadataContent := `name: ubuntu-noble
version: "1.165"
stemcell_formats:
  - docker-light
cloud_properties:
  image_reference: ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165
`
				createTestArchive(stemcellPath, "stemcell.MF", metadataContent)

				realParser := bstem.NewMetadataParser(realFs)
				metadata, isLight, err := realParser.ParseFromPath(stemcellPath)

				Expect(err).NotTo(HaveOccurred())
				Expect(isLight).To(BeTrue())
				Expect(metadata).NotTo(BeNil())
				Expect(metadata.GetDigest()).To(Equal(""))
				Expect(metadata.IsLightStemcell()).To(BeTrue())
			})

			It("should return false for archives without metadata files", func() {
				stemcellPath := filepath.Join(tempDir, "no-metadata.tgz")
				createTestArchive(stemcellPath, "image", "some-image-content")

				realParser := bstem.NewMetadataParser(realFs)
				metadata, isLight, err := realParser.ParseFromPath(stemcellPath)

				Expect(err).NotTo(HaveOccurred())
				Expect(isLight).To(BeFalse())
				Expect(metadata).To(BeNil())
			})

			It("should handle invalid YAML gracefully", func() {
				stemcellPath := filepath.Join(tempDir, "invalid-yaml.tgz")
				metadataContent := `name: invalid
this is not valid yaml: {[}]
`
				createTestArchive(stemcellPath, "stemcell.MF", metadataContent)

				realParser := bstem.NewMetadataParser(realFs)
				_, _, err := realParser.ParseFromPath(stemcellPath)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Unmarshaling metadata"))
			})

			It("should handle non-existent files gracefully", func() {
				realParser := bstem.NewMetadataParser(realFs)
				_, _, err := realParser.ParseFromPath("/non/existent/file.tgz")

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Checking stemcell path"))
			})
		})
	})
})

// Helper function to create a test tar.gz archive
func createTestArchive(path, filename, content string) {
	file, err := os.Create(path)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		err := file.Close()
		Expect(err).NotTo(HaveOccurred())
	}()

	gzipWriter := gzip.NewWriter(file)
	defer func() {
		err := gzipWriter.Close()
		Expect(err).NotTo(HaveOccurred())
	}()

	tarWriter := tar.NewWriter(gzipWriter)
	defer func() {
		err := tarWriter.Close()
		Expect(err).NotTo(HaveOccurred())
	}()

	header := &tar.Header{
		Name: filename,
		Mode: 0600,
		Size: int64(len(content)),
	}

	err = tarWriter.WriteHeader(header)
	Expect(err).NotTo(HaveOccurred())

	_, err = tarWriter.Write([]byte(content))
	Expect(err).NotTo(HaveOccurred())
}
