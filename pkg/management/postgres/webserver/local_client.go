package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

// LocalClient is an entity capable of interacting with the local webserver endpoints
type LocalClient interface {
	// SetWALArchiveStatusCondition sets the wal-archive status condition.
	// An empty errMessage means that the archive process was successful.
	// Returns any error encountered during the request.
	SetWALArchiveStatusCondition(ctx context.Context, errMessage string) error
}

type localClient struct {
	cli *http.Client
}

// NewLocalClient returns a new instance of LocalClient
func NewLocalClient() LocalClient {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	return &localClient{cli: resources.NewHTTPClient(connectionTimeout, requestTimeout)}
}

func (c *localClient) SetWALArchiveStatusCondition(ctx context.Context, errMessage string) error {
	contextLogger := log.FromContext(ctx).WithValues("endpoint", url.PathWALArchiveStatusCondition)

	asr := ArchiveStatusRequest{
		Error: errMessage,
	}

	encoded, err := json.Marshal(&asr)
	if err != nil {
		return err
	}

	resp, err := http.Post(
		url.Local(url.PathWALArchiveStatusCondition, url.LocalPort),
		"application/json",
		bytes.NewBuffer(encoded),
	)
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
