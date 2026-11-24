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
			lock, policy, data, err := lockAndPolicy(projectRoot, cfg)
			if lock == nil || err != nil || policy.Level() != registry.RequireSigned {
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
		Description: "The signers of a package in bino.lock must satisfy every [trust] signer rule that covers it, as install does. A [trust] policy that cannot be loaded is reported here too.",
		Check: func(_ context.Context, _ []Document) []Finding {
			lock, policy, data, err := lockAndPolicy(projectRoot, cfg)
			if lock == nil {
				return nil
			}
			if err != nil {
				return []Finding{{
					RuleID:   "registry-signer-mismatch",
					Message:  "cannot check the signers in " + registry.LockfileName + ": " + err.Error(),
					File:     pathutil.ProjectConfigPath(projectRoot),
					Line:     1,
					Column:   1,
					Severity: "error",
				}}
			}
			var out []Finding
			for _, e := range lock.Packages {
				// The stored signatures show every signer of the version, which
				// a policy with rules in several layers needs; the one signer
				// the lock records stands in when they are gone.
				check, found, err := registry.StoredSignatures(projectRoot, e, policy)
				if err != nil {
					out = append(out, lockFinding("registry-signer-mismatch", projectRoot, data, e.Name,
						fmt.Sprintf("%s@%s: the stored signatures do not verify: %v", e.Name, e.Version, err)))
					continue
				}
				signers := check.Signers()
				if !found {
					signers = nil
					if e.Signer != nil {
						signers = []registry.Signer{*e.Signer}
					}
				}
				if len(signers) == 0 {
					continue
				}
				// Under require = "signed" install refuses a package no rule
				// covers, so lint does too.
				covered, trusted, _ := policy.Evaluate(e.Name, signers)
				switch {
				case covered && !trusted:
					out = append(out, lockFinding("registry-signer-mismatch", projectRoot, data, e.Name,
						fmt.Sprintf("%s@%s is locked as signed by %s, which does not satisfy the [trust] signer rules for it", e.Name, e.Version, signers[0])))
				case !covered && policy.Level() == registry.RequireSigned:
					out = append(out, lockFinding("registry-signer-mismatch", projectRoot, data, e.Name,
						fmt.Sprintf("%s@%s is locked as signed by %s, but no [trust] signer rule covers it and [trust] requires signed packages", e.Name, e.Version, signers[0])))
				}
			}
			return out
		},
	}
}

// lockAndPolicy loads bino.lock and the trust policy. Without a lock, or with
// one that does not parse, there is nothing to check: the registry commands
// report a broken lock. A policy that cannot be loaded is returned as err.
func lockAndPolicy(projectRoot string, cfg *pathutil.ProjectConfig) (*registry.Lockfile, *registry.TrustPolicy, []byte, error) {
	lock, data := loadLock(projectRoot)
	if lock == nil {
		return nil, nil, nil, nil
	}
	var trust pathutil.TrustConfig
	if cfg != nil {
		trust = cfg.Trust
	}
	policy, err := registry.LoadTrustPolicy(projectRoot, trust)
	return lock, policy, data, err
}

// loadLock returns bino.lock and its bytes, or nil when it is missing, does
// not parse or locks nothing.
func loadLock(projectRoot string) (lock *registry.Lockfile, data []byte) {
	data, err := os.ReadFile(filepath.Join(projectRoot, registry.LockfileName))
	if err != nil {
		return nil, nil
	}
	lock, err = registry.LoadLockfile(projectRoot)
	if err != nil || len(lock.Packages) == 0 {
		return nil, nil
	}
	return lock, data
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
