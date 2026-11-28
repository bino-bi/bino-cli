//go:build !unix

package cli

import "errors"

func mkfifo(string) error { return errors.New("no pipes") }
