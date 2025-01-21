package helpers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func DownloadFile(url string) ([]byte, error) {

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("Could not retrieve file from url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected HTTP status: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Could not read the target URLs contents: %w", err)
	}

	return buf.Bytes(), nil

}
