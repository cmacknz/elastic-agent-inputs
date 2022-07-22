// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package input

import (
	"errors"
	"fmt"
	"strings"
)

// LoadError is returned by Loaders in case of failures.
type LoadError struct {
	// Name of input/module that failed to load (if applicable)
	Name string

	// Reason why the loader failed. Can either be the cause reported by the
	// Plugin or some other indicator like ErrUnknown
	Reason error

	// (optional) Message to report in additon.
	Message string
}

// SetupError indicates that the loader initialization has detected
// errors in individual plugin configurations or duplicates.
type SetupError struct {
	Fails []error
}

// ErrUnknownInput indicates that the plugin type does not exist. Either
// because the 'type' setting name does not match the loaders expectations,
// or because the type is unknown.
var ErrUnknownInput = errors.New("unknown input type")

// ErrNoInputConfigured indicates that the 'type' setting is missing.
var ErrNoInputConfigured = errors.New("no input type configured")

// ErrPluginWithoutName reports that the operation failed because
// the plugin is required to have a Name.
var ErrPluginWithoutName = errors.New("the plugin has no name")

// IsUnknownInputError checks if an error value indicates an input load
// error because there is no existing plugin that can create the input.
func IsUnknownInputError(err error) bool { return errors.Is(err, ErrUnknownInput) }

// Unwrap returns the reason if present
func (e *LoadError) Unwrap() error { return e.Reason }

// Error returns the errors string repesentation
func (e *LoadError) Error() string {
	var buf strings.Builder

	if e.Message != "" {
		buf.WriteString(e.Message)
	} else if e.Name != "" {
		buf.WriteString("failed to load ")
		buf.WriteString(e.Name)
	}

	if e.Reason != nil {
		if buf.Len() > 0 {
			buf.WriteString(": ")
		}
		fmt.Fprintf(&buf, "%v", e.Reason)
	}

	if buf.Len() == 0 {
		return "<loader error>"
	}
	return buf.String()
}

// Error returns the errors string repesentation
func (e *SetupError) Error() string {
	var buf strings.Builder
	buf.WriteString("invalid plugin setup found:")
	for _, err := range e.Fails {
		fmt.Fprintf(&buf, "\n\t%v", err)
	}
	return buf.String()
}
