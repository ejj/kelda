package specs

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/robertkrimen/otto"

	"github.com/NetSys/quilt/stitch"
)

func configRunOnce(configPath string, quiltPath string) error {
	stitch.HTTPGet = func(url string) (*http.Response, error) {
		resp := http.Response{
			Body: ioutil.NopCloser(bytes.NewBufferString("")),
		}
		return &resp, nil
	}
	_, err := stitch.FromFile(configPath, stitch.ImportGetter{
		Path: quiltPath,
	})
	return err
}

func TestConfigs(t *testing.T) {
	testConfig := func(configPath string, quiltPath string) {
		if err := configRunOnce(configPath, quiltPath); err != nil {
			errString := err.Error()
			// Print the stacktrace if it's an Otto error.
			if ottoError, ok := err.(*otto.Error); ok {
				errString = ottoError.String()
			}
			t.Errorf("%s failed validation: %s \n quiltPath: %s",
				configPath, errString, quiltPath)
		}
	}

	goPath := os.Getenv("GOPATH")
	quiltPath := filepath.Join(goPath, "src")

	testConfig("../quilt-tester/tests/100-logs/logs.js", quiltPath)
	testConfig("../quilt-tester/tests/20-spark/spark.js", quiltPath)
	testConfig("./example.js", quiltPath)
	testConfig("./spark/sparkPI.js", quiltPath)
	testConfig("./wordpress/wordpress-example.js", quiltPath)
	testConfig("./etcd/etcd-example.js", quiltPath)
	testConfig("./zookeeper/zookeeper-example.js", quiltPath)
	testConfig("./redis/redis-example.js", quiltPath)
	testConfig("./mean/example.js", quiltPath)
}
