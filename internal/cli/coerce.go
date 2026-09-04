package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/runner"
)

// resolveValues turns parsed flags into the values a step is expanded with,
// applying defaults, coercing types and enforcing every constraint the config
// declared. It runs before any process starts, so a bad value costs nothing.
func resolveValues(cmd *cobra.Command, nc *config.NamedCommand) (runner.Values, error) {
	vals := make(runner.Values, len(nc.Params))

	for _, p := range nc.Params {
		name := strings.TrimPrefix(strings.TrimPrefix(p.Flag, "--"), "-")

		v, err := resolveOne(cmd, p, name)
		if err != nil {
			return nil, err
		}
		vals[p.Name] = v
	}

	return vals, nil
}

func resolveOne(cmd *cobra.Command, p config.Param, flagName string) (runner.Value, error) {
	v := runner.Value{Param: p}

	if p.ResolvedType() == config.TypeBool {
		b, err := cmd.Flags().GetBool(flagName)
		if err != nil {
			return v, err
		}
		if !b && p.Default != nil {
			b, _ = p.Default.(bool)
		}
		v.Bool = b
		v.Set = true
		v.Str = strconv.FormatBool(b)
		return v, nil
	}

	raw, err := cmd.Flags().GetString(flagName)
	if err != nil {
		return v, err
	}

	switch {
	case cmd.Flags().Changed(flagName):
		v.Str, v.Set = raw, true
	case p.Default != nil:
		v.Str, v.Set = stringify(p.Default), true
	case p.Required:
		return v, fmt.Errorf("required flag %q is not set", p.Flag)
	default:
		return v, nil
	}

	if err := checkValue(&v, p); err != nil {
		return v, err
	}
	return v, nil
}

// checkValue applies the type rules and any declared pattern.
func checkValue(v *runner.Value, p config.Param) error {
	switch p.ResolvedType() {
	case config.TypeInt:
		if _, err := strconv.ParseInt(v.Str, 10, 64); err != nil {
			return fmt.Errorf("%s expects a whole number, got %q", p.Flag, v.Str)
		}

	case config.TypeNumber:
		if _, err := strconv.ParseFloat(v.Str, 64); err != nil {
			return fmt.Errorf("%s expects a number, got %q", p.Flag, v.Str)
		}

	case config.TypeEnum:
		if !slices.Contains(p.Values, v.Str) {
			return fmt.Errorf("%s expects one of %s, got %q",
				p.Flag, strings.Join(p.Values, ", "), v.Str)
		}

	case config.TypePath:
		expanded, err := expandPath(v.Str)
		if err != nil {
			return err
		}
		v.Str = expanded

		// A required path is checked now rather than left to fail deep inside
		// whatever tool the step runs, where the message is usually worse.
		if p.Required {
			if _, err := os.Stat(v.Str); err != nil {
				return fmt.Errorf("%s: %s does not exist", p.Flag, v.Str)
			}
		}
	}

	if p.Pattern != "" {
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return fmt.Errorf("%s has an invalid pattern in noodge.yaml: %w", p.Flag, err)
		}
		if !re.MatchString(v.Str) {
			return fmt.Errorf("%s must match %s, got %q", p.Flag, p.Pattern, v.Str)
		}
	}

	return nil
}

// expandPath resolves a leading ~ against the home directory.
func expandPath(s string) (string, error) {
	if s != "~" && !strings.HasPrefix(s, "~/") && !strings.HasPrefix(s, `~\`) {
		return s, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~: %w", err)
	}
	if s == "~" {
		return home, nil
	}
	return filepath.Join(home, s[2:]), nil
}

// stringify renders a YAML scalar the way it should appear on a command line.
func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		// A YAML integer often decodes as a float. Rendering 3000 as "3000"
		// rather than "3000.000000" is the difference between a working port
		// number and a baffling one.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
