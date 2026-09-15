package transport

// selfconfig.go implements the configuration every transport plugin resolves
// for ITSELF out of tUserConfig, the way the eligibility plugins already do
// (eligibility/optum.go:loadConfig, eligibility/stedi.go:loadConfig,
// eligibility/bcbs_fhir.go, eligibility/ncmedicaid.go,
// eligibility/medicare_hets.go, eligibility/gatewayedi.go): the plugin finds
// the caller in the context, reads that user's own configuration rows, keeps
// the ones whose namespace identifies this plugin, and applies them through its
// own option-coercion path.
//
// Why: nothing in production calls SetOptions (jobqueue only calls SetContext,
// jobqueue/jobqueue.go:325), so before this file every transport ran with zero
// options and failed its own validation (sftp.go, gatewayedi.go) or dialled an
// empty endpoint (claimlogic.go) instead of delivering anything. The values were
// in the database all along, seeded by migrations/001_legacy.up.sql:62-70.
//
// The rules, identical for every plugin that implements them:
//
//   - A row belongs to this plugin when its namespace is either the Java FQCN
//     the legacy system stores (the first, authoritative form - that is the
//     only form the database and the UI have ever written) or the registered
//     short name. The FQCN is matched first and wins if both forms carry the
//     same option.
//   - A row for another user is never applied, and neither is a row under
//     another plugin's namespace, nor an option name this plugin does not
//     accept in Options() (a legacy namespace can hold another tool's keys:
//     'org.remitt.plugin.transport.ScriptedHttpTransport' carries
//     username/password, which the Script transport does not read).
//   - An option the caller set explicitly through SetOptions always beats the
//     value stored for the user; the database fills in the rest.
//   - A stored value that cannot be read as the option's type is reported as a
//     configuration error NAMING THE OPTION, never dropped silently.
//   - No user in the context, or no database, is not itself a failure: there is
//     simply no database-derived configuration, the plugin keeps whatever
//     SetOptions supplied, and the plugin's own validation reports the gap
//     exactly as it did before self-configuration existed.
//
// StoreFile and StoreFilePdf declare no options at all (Options() == []), so
// there is nothing for them to load; their own database guard stays where it is.

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

// transportNamespaces returns the tUserConfig namespaces a transport plugin's
// rows may live under, most authoritative first.
//
// The Java FQCN comes first because it is the only form the legacy database and
// the UI have ever stored (migrations/001_legacy.up.sql:63-69,
// ui/testHarness.html); the short name is the Go name jobqueue and the tests use
// for the same plugin. Both are registered in the plugin registry (map.go), and
// TestSelfConfig_NamespacesMatchTheRegistry pins that this list cannot drift
// away from it.
func transportNamespaces(shortName, javaClass string) []string {
	return []string{JavaPluginPrefix + javaClass, shortName}
}

// optionValueConverter renders one stored tUserConfig value into the Go value
// the plugin's own option-coercion path (SetOptions) expects for that option
// name.
//
// tUserConfig.cValue is a BLOB, so every stored value arrives as text: a string
// option is passed through unchanged, and an option whose field is an int is
// parsed here. Letting SetOptions do the parsing instead would be wrong twice
// over - it would reject the perfectly valid text "22" for sftpPort (the exact
// value the legacy seed stores, migrations/001_legacy.up.sql:65) and it would
// accept nothing else either, since the database has no type information to
// hand it.
type optionValueConverter func(option, stored string) (any, error)

// storedStringOption is the converter every string option uses: the stored text
// is the value.
func storedStringOption(_ string, stored string) (any, error) {
	return stored, nil
}

// storedIntOption converts one stored configuration value into an int. A value
// that is empty or not an integer is reported, naming the option: an int
// option that silently stayed 0 is exactly the failure mode this whole file
// exists to remove (a stored cookie for sftpPort would otherwise leave the
// plugin looking unconfigured, and the error the operator sees would never
// mention sftpPort).
func storedIntOption(option, stored string) (any, error) {
	n, err := strconv.Atoi(strings.TrimSpace(stored))
	if err != nil {
		return nil, fmt.Errorf("option '%s': stored value %q is not an integer", option, stored)
	}
	return n, nil
}

// loadUserConfigOptions reads the caller's own rows out of tUserConfig and
// returns them as an option map ready for the plugin's SetOptions.
//
// It returns a nil map (and a nil error) when there is no configuration to
// load: no user in the context, or no database. Both are recorded in the log
// and are deliberately not failures - a transfer that carries no identity
// (sftp) and a direct caller that supplied every option itself (tests, callers
// of SetOptions) must keep working, and a process whose database was never
// initialised must behave exactly as it did before this file existed, with the
// plugin's own validation reporting the configuration that is missing.
//
// A database that IS present but fails is a configuration error: the plugin
// cannot know what it was configured to do, so it must not guess.
func loadUserConfigOptions(ctx context.Context, plugin string, namespaces, accepted []string, convert optionValueConverter) (map[string]any, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}

	um, ok := user.FromContext(ctx)
	if !ok {
		log.Printf("%s: no user in the context: tUserConfig cannot be consulted, only explicitly set options apply", plugin)
		return nil, nil
	}
	if model.Queries == nil {
		log.Printf("%s: user %q: database is not initialised: tUserConfig cannot be consulted, only explicitly set options apply", plugin, um.Username)
		return nil, nil
	}

	rows, err := model.GetConfigValues(um.Username)
	if err != nil {
		return nil, fmt.Errorf("loading configuration for user %q: %w", um.Username, err)
	}

	stored := map[string]any{}
	fromFQCN := map[string]bool{}
	for _, row := range rows {
		// GetConfigValues already selects WHERE user = ?, so a row that names
		// somebody else is a row this plugin has no business applying.
		if row.User != um.Username {
			continue
		}

		rank := -1
		for i, ns := range namespaces {
			if row.Namespace == ns {
				rank = i
				break
			}
		}
		if rank < 0 {
			continue // another plugin's namespace
		}
		if !containsString(accepted, row.Option) {
			continue // not an option this plugin reads
		}
		if rank > 0 && fromFQCN[row.Option] {
			continue // the Java FQCN row wins over the short-name row
		}

		value, err := convert(row.Option, row.Value)
		if err != nil {
			return nil, err
		}
		stored[row.Option] = value
		if rank == 0 {
			fromFQCN[row.Option] = true
		}
	}

	if len(stored) == 0 {
		return nil, nil
	}
	return stored, nil
}

// mergeOptions overlays the explicitly-set options on top of the ones read from
// the database, which is the documented precedence: an option a caller set with
// SetOptions always wins over the value stored for the user, and the database
// supplies only the options the caller did not set.
func mergeOptions(fromUserConfig, explicit map[string]any) map[string]any {
	if len(fromUserConfig) == 0 && len(explicit) == 0 {
		return nil
	}
	merged := make(map[string]any, len(fromUserConfig)+len(explicit))
	for k, v := range fromUserConfig {
		merged[k] = v
	}
	for k, v := range explicit {
		merged[k] = v
	}
	return merged
}

// copyOptions takes a snapshot of an option map, so a caller that mutates the
// map it handed to SetOptions cannot change what the plugin later treats as
// explicitly set.
func copyOptions(o map[string]any) map[string]any {
	if len(o) == 0 {
		return nil
	}
	c := make(map[string]any, len(o))
	for k, v := range o {
		c[k] = v
	}
	return c
}

// containsString reports whether list holds name.
func containsString(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}

// userConfigurableTransporter is implemented by every transport plugin that
// resolves its own configuration from tUserConfig. It exists so the namespace
// each plugin matches on can be pinned against the registry in one test
// (TestSelfConfig_NamespacesMatchTheRegistry).
type userConfigurableTransporter interface {
	// userConfigNamespaces returns the plugin's own tUserConfig namespaces,
	// FQCN first, then the registered short name.
	userConfigNamespaces() []string
}
