package client

import "errors"

type pluginError struct {
	innerErr error
}

func (e *pluginError) Error() string {
	return e.innerErr.Error()
}

// ToPluginError converts a generic error to a plugin error.
func ToPluginError(err error) error {
	return &pluginError{
		innerErr: err,
	}
}

// IsPluginError checks if the error is a plugin error.
func IsPluginError(err error) bool {
	if err == nil {
		return false
	}

	var pluginErr *pluginError
	return errors.As(err, &pluginErr)
}
