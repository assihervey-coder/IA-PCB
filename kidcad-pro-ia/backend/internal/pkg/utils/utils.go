// Package utils provides small shared helpers (IDs, unit conversions,
// numeric clamping) with no domain dependencies.
package utils

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// NewID returns a random UUID v4 string. If crypto/rand unexpectedly fails,
// a time-based fallback identifier is produced instead of panicking.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := uint64(time.Now().UnixNano())
		binary.LittleEndian.PutUint64(b[0:8], n)
		binary.LittleEndian.PutUint64(b[8:16], uint64(n^0x9e3779b97f4a7c15))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// MmToMil converts millimeters to mils (1/1000 inch).
func MmToMil(mm float64) float64 { return mm / 0.0254 }

// MilToMm converts mils to millimeters.
func MilToMm(mil float64) float64 { return mil * 0.0254 }

// MinF returns the smallest of a and b.
func MinF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// MaxF returns the largest of a and b.
func MaxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// RoundTo rounds v to the given number of decimal places.
func RoundTo(v float64, decimals int) float64 {
	if decimals < 0 {
		decimals = 0
	}
	shift := math.Pow(10, float64(decimals))
	return math.Round(v*shift) / shift
}
