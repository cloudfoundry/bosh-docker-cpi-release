package stemcell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	dkrimages "github.com/docker/docker/api/types/image"
	dkrclient "github.com/docker/docker/client"
)

func TestImportUsesVerifiedImageSnapshot(t *testing.T) {
	imageID := "sha256:" + strings.Repeat("a", 64)
	expected := "sha256:" + strings.Repeat("b", 64)
	other := "sha256:" + strings.Repeat("c", 64)
	for _, tc := range []struct {
		name     string
		expected string
		verify   bool
		wantOK   bool
	}{{"manifest digest", expected, true, true}, {"image config digest", imageID, true, true}, {"plain digest", strings.TrimPrefix(expected, "sha256:"), true, true}, {"mismatch", "sha256:" + strings.Repeat("d", 64), true, false}, {"missing", "", true, false}, {"explicit disabled", "", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			inspections := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/images/create"):
					if _, err := fmt.Fprintln(w, `{"status":"complete"}`); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "/json"):
					inspections++
					current := dkrimages.InspectResponse{ID: imageID, RepoDigests: []string{"registry.example/other@" + other, "registry.example/stemcell@" + expected}}
					// A second lookup by tag observes an unrelated image after concurrent retag.
					if inspections > 1 {
						current.ID = other
						current.RepoDigests = []string{"registry.example/stemcell@" + other}
					}
					if err := json.NewEncoder(w).Encode(current); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected Docker operation %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := dkrclient.NewClientWithOpts(dkrclient.WithHost(server.URL), dkrclient.WithVersion("1.47"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Error(err)
				}
			}()
			dir := t.TempDir()
			manifest := fmt.Sprintf("stemcell_formats: [docker-light]\ncloud_properties:\n  image_reference: registry.example/stemcell:latest\n  digest: %q\n", tc.expected)
			if err := os.WriteFile(filepath.Join(dir, "stemcell.MF"), []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			logger := boshlog.NewLogger(boshlog.LevelNone)
			importer := NewLightImporter(client, boshsys.NewOsFileSystem(logger), nil, tc.verify, logger)
			image, err := importer.ImportFromPath(dir)
			if !tc.wantOK {
				if err == nil {
					t.Fatal("accepted unverified image")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if image.ID().AsString() != imageID {
				t.Fatalf("CID %s does not identify inspected image %s", image.ID().AsString(), imageID)
			}
			if inspections != 1 {
				t.Fatalf("inspected mutable tag %d times", inspections)
			}
		})
	}
}
