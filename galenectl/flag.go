package main

import (
	"strconv"
	"time"
)

// stringOption represents a string command-line option that may be unset
type stringOption struct {
	set   bool
	value string
}

func (o *stringOption) Set(value string) error {
	o.value = value
	o.set = true
	return nil
}

func (o *stringOption) String() string {
	if o == nil {
		return "(nil)"
	}
	if !o.set {
		return "(unset)"
	}
	return o.value
}

// boolOption represents a boolean command-line option that may be unset
type boolOption struct {
	set   bool
	value bool
}

func (o *boolOption) Set(value string) error {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	o.value = v
	o.set = true
	return nil
}

func (o *boolOption) String() string {
	if o == nil {
		return "(nil)"
	}
	if !o.set {
		return "(unset)"
	}
	return strconv.FormatBool(o.value)
}

func (o *boolOption) IsBoolFlag() bool {
	return true
}

// durationOption represents a [time.Duration] command-line option
// that may be unset.
type durationOption struct {
	set   bool
	value time.Duration
}

func (o *durationOption) Set(value string) error {
	v, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	o.value = v
	o.set = true
	return nil
}

func (o *durationOption) String() string {
	if o == nil {
		return "(nil)"
	}
	if !o.set {
		return "(unset)"
	}
	return o.value.String()
}
