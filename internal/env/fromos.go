package env

import "os"

// FromOS builds an Env from the real process environment. This file and
// cmd/ are the only places allowed to read HOME / XDG_CONFIG_HOME.
func FromOS() (Env, error) {
	home := os.Getenv("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Env{}, err
		}
		home = h
	}
	return New(home, os.Getenv("XDG_CONFIG_HOME")), nil
}

// RealHome returns the real home directory of the current user, for test
// guards only ("never touch this").
func RealHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
