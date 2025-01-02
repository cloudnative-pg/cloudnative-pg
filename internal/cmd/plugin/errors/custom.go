package errors

import "fmt"

// PluginError is a type that allows us to define a custom exit code for the error
type PluginError struct {
	Code  int
	error error
}

func (e *PluginError) Error() string {
	return e.error.Error()
}

// NewKubeAPIServerError returns a new PluginError with an exit code of 2
func NewKubeAPIServerError(err error) *PluginError {
	return &PluginError{
		Code:  2,
		error: fmt.Errorf("while interacting with the kube-api server: %w", err),
	}
}
