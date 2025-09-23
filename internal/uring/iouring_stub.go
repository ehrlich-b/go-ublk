//go:build !giouring
// +build !giouring

package uring

import "fmt"

// NewRealRing is available when built with -tags giouring
func NewRealRing(config Config) (Ring, error) {
	return nil, fmt.Errorf("giouring not enabled; build with -tags giouring")
}
