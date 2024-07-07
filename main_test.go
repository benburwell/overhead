package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestAltitudeToWords(t *testing.T) {
	tests := []struct {
		alt float64
		exp string
	}{
		{100, "one hundred"},
		{900, "niner hundred"},
		{1000, "one thousand"},
		{1100, "one thousand one hundred"},
		{9999, "niner thousand niner hundred"},
		{10000, "one zero thousand"},
		{11000, "one one thousand"},
		{11220, "one one thousand two hundred"},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%f", test.alt), func(t *testing.T) {
			actual := strings.Join(altitudeToWords(test.alt), " ")
			if actual != test.exp {
				t.Errorf("unexpected verbalization: %s", actual)
			}
		})
	}
}

func TestIdentToWords(t *testing.T) {
	tests := []struct {
		ident string
		exp   string
	}{
		{"UAL1234", "united 12 34"},
		{"FDX7123", "fedex 71 23"},
		{"FDX1", "fedex one"},
		{"FDX12", "fedex 12"},
		{"FDX123", "fedex 1 23"},
		{"FDX12345", "fedex one two three four five"},
		{"UAL12A", "united one two alpha"},
		{"N12345", "november one two three four five"},
		{"ZZZ10", "zulu zulu zulu one zero"},
	}
	for _, test := range tests {
		t.Run(test.ident, func(t *testing.T) {
			actual := strings.Join(identToWords(test.ident), " ")
			if actual != test.exp {
				t.Errorf("unexpected verbalization: %s", actual)
			}
		})
	}
}
