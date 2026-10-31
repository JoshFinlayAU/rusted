package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrInvalid is wrapped by the validation errors below, so callers (the HTTP
// API) can tell a bad request from a storage failure.
var ErrInvalid = errors.New("invalid")

// ValidateDeviceName checks that name can safely become a file name in the
// backup repository ([group/]NAME.cfg). A name is a single path segment: no
// separators, not "." or "..", no leading "-" (git would read it as an option)
// and no control characters.
func ValidateDeviceName(name string) error {
	if name == "" {
		return fmt.Errorf("%w device name: must not be empty", ErrInvalid)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf(`%w device name %q: must not contain "/" or "\"`, ErrInvalid, name)
	}
	if err := checkSegment(name); err != nil {
		return fmt.Errorf("%w device name %q: %v", ErrInvalid, name, err)
	}
	return nil
}

// ValidateGroup checks a device group, the sub-directory its config is stored
// under ("" is the repository root). Groups may nest with "/" ("site/core") but
// must stay inside the repository: relative, no empty, "." or ".." segments, no
// "\" and never the .git directory.
func ValidateGroup(group string) error {
	if group == "" {
		return nil
	}
	if strings.Contains(group, `\`) {
		return fmt.Errorf(`%w group %q: must not contain "\"`, ErrInvalid, group)
	}
	if strings.HasPrefix(group, "/") {
		return fmt.Errorf("%w group %q: must be a relative path", ErrInvalid, group)
	}
	for _, seg := range strings.Split(group, "/") {
		if seg == "" {
			return fmt.Errorf("%w group %q: empty path segment", ErrInvalid, group)
		}
		if err := checkSegment(seg); err != nil {
			return fmt.Errorf("%w group %q: %v", ErrInvalid, group, err)
		}
		if strings.EqualFold(seg, ".git") {
			return fmt.Errorf("%w group %q: must not use the .git directory", ErrInvalid, group)
		}
	}
	return nil
}

// validateDevice checks the fields of d that are written on create and update.
func validateDevice(d *Device) error {
	if err := ValidateDeviceName(d.Name); err != nil {
		return err
	}
	if err := ValidateGroup(d.Group); err != nil {
		return err
	}
	if d.Host == "" {
		return fmt.Errorf("%w host: must not be empty", ErrInvalid)
	}
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("%w port %d: must be between 1 and 65535", ErrInvalid, d.Port)
	}
	return nil
}

// checkSegment holds the rules shared by names and group segments.
func checkSegment(seg string) error {
	if seg == "." || seg == ".." {
		return errors.New(`"." and ".." are not allowed`)
	}
	if strings.HasPrefix(seg, "-") {
		return errors.New(`must not start with "-"`)
	}
	if strings.IndexFunc(seg, unicode.IsControl) >= 0 {
		return errors.New("must not contain control characters")
	}
	return nil
}

// ConfigPath is the repository-relative path of a device's config file. Both
// parts are assumed to have passed ValidateDeviceName / ValidateGroup.
func ConfigPath(name, group string) string {
	if group == "" {
		return name + ".cfg"
	}
	return group + "/" + name + ".cfg"
}

// ConfigPath is the repository-relative path of d's config file.
func (d *Device) ConfigPath() string { return ConfigPath(d.Name, d.Group) }
