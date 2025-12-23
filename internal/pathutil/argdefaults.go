package pathutil

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ArgResolver helps resolve command-line arguments with defaults from bino.toml.
// When an explicit CLI flag is provided, it overrides the TOML default and logs an info message.
type ArgResolver struct {
	cmd     *cobra.Command
	args    CommandArgs
	logInfo func(format string, args ...any)
}

// NewArgResolver creates a new ArgResolver for the given command and TOML args.
// The logInfo function is called when a CLI flag overrides a TOML default.
func NewArgResolver(cmd *cobra.Command, args CommandArgs, logInfo func(format string, args ...any)) *ArgResolver {
	return &ArgResolver{
		cmd:     cmd,
		args:    args,
		logInfo: logInfo,
	}
}

// ResolveString returns the effective value for a string flag.
// If the flag was explicitly set, it returns the CLI value and logs an override message.
// Otherwise, it returns the TOML default (if any) or the provided fallback.
func (r *ArgResolver) ResolveString(flagName, tomlKey, fallback string) string {
	flag := r.cmd.Flags().Lookup(flagName)
	if flag != nil && flag.Changed {
		tomlVal, hasToml := r.args.GetString(tomlKey)
		if hasToml && flag.Value.String() != tomlVal {
			r.logInfo("Overriding %s from bino.toml (%q -> %q)", tomlKey, tomlVal, flag.Value.String())
		}
		return flag.Value.String()
	}

	if val, ok := r.args.GetString(tomlKey); ok {
		return val
	}
	return fallback
}

// ResolveInt returns the effective value for an int flag.
// If the flag was explicitly set, it returns the CLI value and logs an override message.
// Otherwise, it returns the TOML default (if any) or the provided fallback.
func (r *ArgResolver) ResolveInt(flagName, tomlKey string, fallback int) int {
	flag := r.cmd.Flags().Lookup(flagName)
	if flag != nil && flag.Changed {
		tomlVal, hasToml := r.args.GetInt(tomlKey)
		if hasToml {
			r.logInfo("Overriding %s from bino.toml (%d -> %s)", tomlKey, tomlVal, flag.Value.String())
		}
		// Parse from flag value
		var val int
		if _, err := parseIntFlag(flag.Value.String(), &val); err == nil {
			return val
		}
		return fallback
	}

	if val, ok := r.args.GetInt(tomlKey); ok {
		return val
	}
	return fallback
}

// ResolveBool returns the effective value for a bool flag.
// If the flag was explicitly set, it returns the CLI value and logs an override message.
// Otherwise, it returns the TOML default (if any) or the provided fallback.
func (r *ArgResolver) ResolveBool(flagName, tomlKey string, fallback bool) bool {
	flag := r.cmd.Flags().Lookup(flagName)
	if flag != nil && flag.Changed {
		tomlVal, hasToml := r.args.GetBool(tomlKey)
		if hasToml {
			r.logInfo("Overriding %s from bino.toml (%v -> %s)", tomlKey, tomlVal, flag.Value.String())
		}
		return flag.Value.String() == "true"
	}

	if val, ok := r.args.GetBool(tomlKey); ok {
		return val
	}
	return fallback
}

// ResolveStringSlice returns the effective value for a string slice flag.
// If the flag was explicitly set, it returns the CLI value and logs an override message.
// Otherwise, it returns the TOML default (if any) or the provided fallback.
func (r *ArgResolver) ResolveStringSlice(flagName, tomlKey string, fallback []string) []string {
	flag := r.cmd.Flags().Lookup(flagName)
	if flag != nil && flag.Changed {
		tomlVal, hasToml := r.args.GetStringSlice(tomlKey)
		if hasToml && len(tomlVal) > 0 {
			r.logInfo("Overriding %s from bino.toml", tomlKey)
		}
		// Get the slice value from the flag
		if sliceVal, ok := flag.Value.(interface{ GetSlice() []string }); ok {
			return sliceVal.GetSlice()
		}
		return fallback
	}

	if val, ok := r.args.GetStringSlice(tomlKey); ok {
		return val
	}
	return fallback
}

// parseIntFlag parses an int from a flag value string.
func parseIntFlag(s string, target *int) (bool, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return false, err
	}
	*target = n
	return true, nil
}
