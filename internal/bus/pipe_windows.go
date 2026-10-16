// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package bus

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"github.com/fqazzazee/shuttle/internal/activity"
	"github.com/fqazzazee/shuttle/internal/daemon"
)

// The pipe carries one JSON value per line each way. A client sends
// requests; the daemon answers each with a reply of the same ID. A client
// that asked to watch gets events instead, one per status change, until it
// hangs up.

// request is a call to the daemon. Which fields mean anything depends on
// the method.
type request struct {
	ID     uint64 `json:"id"`
	Method string `json:"method"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Choice string `json:"choice,omitempty"`
	On     bool   `json:"on,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// reply answers a request, or, with ID 0, is an event.
type reply struct {
	ID    uint64     `json:"id,omitempty"`
	Error *wireError `json:"error,omitempty"`

	Shuttles    []daemon.Status `json:"shuttles,omitempty"`
	Activity    []entry         `json:"activity,omitempty"`
	ConfigError string          `json:"config_error,omitempty"`
	State       daemon.State    `json:"state,omitempty"`
	Paused      bool            `json:"paused,omitempty"`
	Up          int64           `json:"up,omitempty"`
	Down        int64           `json:"down,omitempty"`

	Changed *daemon.Status `json:"changed,omitempty"` // an event
}

// entry is an activity log entry on the pipe, its shuttle's name included: a
// log's own JSON leaves it out, a log being one shuttle's.
type entry struct {
	activity.Entry
	Shuttle string `json:"shuttle"`
}

// wireError is a daemon error on the pipe: its code is one of the codes
// below, which the client turns back into the daemon's errors.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	codeUnknownShuttle = "unknown-shuttle"
	codeDisabled       = "disabled"
	codeConfig         = "config"
	codeFailed         = "failed"
)

// The methods.
const (
	methodShuttles       = "shuttles"
	methodSyncNow        = "sync-now"
	methodSetPaused      = "set-paused"
	methodResolve        = "resolve"
	methodSetEnabled     = "set-enabled"
	methodAllowDeletions = "allow-deletions"
	methodActivity       = "activity"
	methodReload         = "reload"
	methodConfigError    = "config-error"
	methodState          = "state"
	methodRates          = "rates"
	methodWatch          = "watch"
)

func toWire(err error) *wireError {
	if err == nil {
		return nil
	}
	code := codeFailed
	switch {
	case errors.Is(err, daemon.ErrUnknownShuttle):
		code = codeUnknownShuttle
	case errors.Is(err, daemon.ErrDisabled):
		code = codeDisabled
	case errors.Is(err, daemon.ErrConfig):
		code = codeConfig
	}
	return &wireError{Code: code, Message: err.Error()}
}

func fromWire(e *wireError) error {
	if e == nil {
		return nil
	}
	switch e.Code {
	case codeUnknownShuttle:
		return fmt.Errorf("%s: %w", e.Message, daemon.ErrUnknownShuttle)
	case codeDisabled:
		return fmt.Errorf("%s: %w", e.Message, daemon.ErrDisabled)
	case codeConfig:
		return fmt.Errorf("%s: %w", e.Message, daemon.ErrConfig)
	}
	return errors.New(e.Message)
}

// PipeName is this user's pipe: \\.\pipe\shuttle-<the user's SID>, one per
// user on the machine. SHUTTLE_PIPE names another instead: tests set it, so
// that none reaches the user's own daemon.
func PipeName() (string, error) {
	if p := os.Getenv("SHUTTLE_PIPE"); p != "" {
		return p, nil
	}
	sid, err := userSID()
	if err != nil {
		return "", err
	}
	return `\\.\pipe\shuttle-` + sid, nil
}

// pipeSecurity lets the user, and no one else, open the pipe; the listener
// refuses clients on other machines besides.
func pipeSecurity() (string, error) {
	sid, err := userSID()
	if err != nil {
		return "", err
	}
	return "D:P(A;;GA;;;" + sid + ")", nil
}

func userSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("who this user is: %w", err)
	}
	return user.User.Sid.String(), nil
}
