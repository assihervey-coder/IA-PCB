package schematic

import (
	"errors"
	"strings"
)

// NetClass groups nets sharing electrical constraints
// ("default", "power", "high-speed", "signal").
type NetClass string

const (
	ClassDefault   NetClass = "default"
	ClassPower     NetClass = "power"
	ClassHighSpeed NetClass = "high-speed"
	ClassSignal    NetClass = "signal"
)

// ErrEmptyNetName is raised when a net has a blank name.
var ErrEmptyNetName = errors.New("schéma : nom de net vide")

// PinRef identifies one pin of one component ("R1", "2").
type PinRef struct {
	ComponentRef string
	PinNumber    string
}

// Net is a logical connection set: all pins sharing the same electrical node.
type Net struct {
	Name        string
	Class       NetClass
	Connections []PinRef
}

// Contains reports whether the (component, pin) pair belongs to the net.
func (n *Net) Contains(componentRef, pinNumber string) bool {
	for _, c := range n.Connections {
		if c.ComponentRef == componentRef && c.PinNumber == pinNumber {
			return true
		}
	}
	return false
}

// Validate checks the intrinsic consistency of the net.
func (n *Net) Validate() error {
	if strings.TrimSpace(n.Name) == "" {
		return ErrEmptyNetName
	}
	return nil
}
