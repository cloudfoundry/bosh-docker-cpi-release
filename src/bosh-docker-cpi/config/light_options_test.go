package config_test

import (
	"bosh-docker-cpi/config"
	"encoding/json"
	"testing"
)

func TestImageVerificationDefaults(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{{`{}`, true}, {`{"light_stemcell":{}}`, true}, {`{"light_stemcell":null}`, true}, {`{"light_stemcell":{"require_image_verification":null}}`, true}, {`{"light_stemcell":{"require_image_verification":true}}`, true}, {`{"light_stemcell":{"require_image_verification":false}}`, false}} {
		var cfg config.Config
		if err := json.Unmarshal([]byte(tc.data), &cfg); err != nil {
			t.Fatal(err)
		}
		if got := cfg.LightStemcell.ImageVerificationRequired(); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.data, got, tc.want)
		}
	}
}
