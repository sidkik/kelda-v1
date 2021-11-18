package assert

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/sidkik/kelda-v1/pkg/errors"
)

type Assertion func() error

func httpGet(endpoint string) (string, error) {
	resp, err := http.Get(endpoint)
	if err != nil {
		return "", errors.WithContext(err, "get")
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.WithContext(err, "read")
	}

	return string(body), nil
}

func HTTPGetShouldNotContain(endpoint, str string) Assertion {
	return func() error {
		actual, err := httpGet(endpoint)
		if err != nil {
			return errors.WithContext(err, "get")
		}

		if strings.Contains(actual, str) {
			return fmt.Errorf("unexpected response: expected %q to not contain %q",
				actual, str)
		}
		return nil
	}
}

func HTTPGetShouldContain(endpoint, exp string) Assertion {
	return func() error {
		actual, err := httpGet(endpoint)
		if err != nil {
			return errors.WithContext(err, "get")
		}

		if !strings.Contains(actual, exp) {
			return fmt.Errorf("unexpected response: expected %q to contain %q",
				actual, exp)
		}
		return nil
	}
}

func HTTPGetShouldEqual(endpoint, exp string) Assertion {
	return func() error {
		actual, err := httpGet(endpoint)
		if err != nil {
			return errors.WithContext(err, "get")
		}

		if exp != actual {
			return fmt.Errorf("unexpected response: expected %q to equal %q",
				exp, actual)
		}
		return nil
	}
}
