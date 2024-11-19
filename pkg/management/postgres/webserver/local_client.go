package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

// LocalClient is a struct capable of interacting with the local webserver endpoints
type LocalClient interface {
	// SetPgStatusArchive sets the status of the archive, an empty errMessage means that the archive process
	// was successful
	SetPgStatusArchive(ctx context.Context, errMessage string) error
}

type localClient struct {
	cli *http.Client
}

// NewLocalClient creates a client capable of interacting with the instance backup endpoints
func NewLocalClient() LocalClient {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	return &localClient{cli: resources.NewHTTPClient(connectionTimeout, requestTimeout)}
}

func (c *localClient) SetPgStatusArchive(ctx context.Context, errMessage string) error {
	contextLogger := log.FromContext(ctx)

	body, err := c.createSetPgStatusArchiveRequestBody(errMessage)
	if err != nil {
		return err
	}

	resp, err := http.Post(url.Local(url.PathPgStatusArchive, url.LocalPort), "application/json", body)
	if err != nil {
		return err
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			contextLogger.Error(err, "while closing response body")
		}
	}()

	return nil
}

func (c *localClient) createSetPgStatusArchiveRequestBody(errMessage string) (io.Reader, error) {
	asr := ArchiveStatusRequest{
		Error: errMessage,
	}

	encoded, err := json.Marshal(&asr)
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(encoded), nil
}
