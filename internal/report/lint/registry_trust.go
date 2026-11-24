package lint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
)

// registryTrustRules check bino.lock against the [trust] policy offline. They
// read the signer the lock pins, not the signatures themselves: 'bino
// registry install' and 'verify' check those. Both rules are always present so
// [lint] can name them; without a lock they find nothing.
func registryTrustRules(projectRoot string, cfg *pathutil.ProjectConfig) []Rule {
	return []Rule{
		registryUnsignedDependency(projectRoot, cfg),
		registrySignerMismatch(projectRoot, cfg),
	}
}

func registryUnsignedDependency(projectRoot string, cfg *pathutil.ProjectConfig) Rule {
	return Rule{
		ID:          "registry-unsigned-dependency",
		Name:        "Registry Unsigned Dependency",
		Description: "Under [trust] require = \"signed\", every package in bino.lock must record a verified signer.",
		Check: func(_ context.Context, _ []Document) []Finding {
			lock, policy, data := lockAndPolicy(projectRoot, cfg)
			if lock == nil || policy.Level() != registry.RequireSigned {
				return nil
			}
			var out []Finding
			for _, e := range lock.Packages {
				if e.Signer != nil {
					continue
				}
				out = append(out, lockFinding("registry-unsigned-dependency", projectRoot, data, e.Name,
					fmt.Sprintf("%s@%s is not signed, but [trust] requires signed packages", e.Name, e.Version)))
			}
			return out
		},
	}
}

func registrySignerMismatch(projectRoot string, cfg *pathutil.ProjectConfig) Rule {
	return Rule{
		ID:          "registry-signer-mismatch",
		Name:        "Registry Signer Mismatch",
		Description: "The signer bino.lock records for a package must satisfy a [trust] signer rule that covers it.",
		Check: func(_ context.Context, _ []Document) []Finding {
			lock, policy, data := lockAndPolicy(projectRoot, cfg)
			if lock == nil {
				return nil
			}
			var out []Finding
			for _, e := range lock.Packages {
				if e.Signer == nil {
					continue
				}
				if covered, ok := policy.MatchesAny(e.Name, *e.Signer); covered && !ok {
					out = append(out, lockFinding("registry-signer-mismatch", projectRoot, data, e.Name,
						fmt.Sprintf("%s@%s is locked as signed by %s, which no [trust] signer rule for it accepts", e.Name, e.Version, e.Signer)))
				}
			}
			return out
		},
	}
}

// lockAndPolicy loads bino.lock and the trust policy. Either failing makes
// the rules silent: the registry commands report a broken lock or policy.
func lockAndPolicy(projectRoot string, cfg *pathutil.ProjectConfig) (*registry.Lockfile, *registry.TrustPolicy, []byte) {
	data, err := os.ReadFile(filepath.Join(projectRoot, registry.LockfileName))
	if err != nil {
		return nil, nil, nil
	}
	lock, err := registry.LoadLockfile(projectRoot)
	if err != nil || len(lock.Packages) == 0 {
		return nil, nil, nil
	}
	var trust pathutil.TrustConfig
	if cfg != nil {
		trust = cfg.Trust
	}
	policy, err := registry.LoadTrustPolicy(projectRoot, trust)
	if err != nil {
		return nil, nil, nil
	}
	return lock, policy, data
}

// lockFinding anchors a finding on the package's name line in bino.lock.
func lockFinding(id, projectRoot string, lockData []byte, name, msg string) Finding {
	line := 1
	for i, l := range strings.Split(string(lockData), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "name") && (strings.Contains(t, "'"+name+"'") || strings.Contains(t, `"`+name+`"`)) {
			line = i + 1
			break
		}
	}
	return Finding{
		RuleID:   id,
		Message:  msg,
		File:     filepath.Join(projectRoot, registry.LockfileName),
		Line:     line,
		Column:   1,
		Severity: "error",
	}
}
