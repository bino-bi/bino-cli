package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

func newRegistryTrustCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage the package signers pinned in bino.lock",
		Long: `Once bino.lock pins who signed a package, a version signed by someone
else, or not signed at all, is refused at every [trust] require level. The
pin stays when the package leaves bino.lock, so a package that comes back is
held to it too. The subcommands here are how a human accepts such a change.`,
	}
	cmd.AddCommand(newRegistryTrustAcceptCommand())
	return cmd
}

func newRegistryTrustAcceptCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "accept <package[@version|@tag]>",
		Short: "Accept a new signer for a locked package",
		Long: `Resolves the package, verifies its signature, and pins the new signer in
bino.lock, printing the old and the new one. The [trust] policy still applies:
this only waives the pin.

Without a version, a declared dependency is re-resolved like
'bino registry update <package>', and a transitive one through the direct
dependencies that pull it in. With a version or tag, a declared dependency is
moved to it like 'bino registry add'.

A package that is pinned but neither declared nor locked (it was removed, or
left the closure) cannot be resolved on its own: its pin is removed instead,
and the next 'bino registry add' or 'update' that brings it back pins the
signer it verifies.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			name, before, after, res, err := registryTrustAccept(cmd.Context(), p, args[0])
			if errors.Is(err, errPinDropped) {
				p.Out.List("old signer: " + signerText(before))
				p.Out.Success(fmt.Sprintf("Removed the signer pin of %s from %s; the next add or update that brings it back pins its signer", name, registry.LockfileName))
				return nil
			}
			if err != nil {
				return err
			}
			for _, c := range res.Changes {
				if c.Name == name && c.Before != c.After {
					p.Out.List(fmt.Sprintf("%s %s -> %s", c.Name, c.Before, c.After))
				}
			}
			for _, w := range res.Warnings {
				p.Out.Warning(w)
			}
			p.Out.List("old signer: " + signerText(before))
			p.Out.List("new signer: " + signerText(after))
			p.Out.Success(fmt.Sprintf("Pinned the signer of %s in %s", name, registry.LockfileName))
			return nil
		},
	}
}

func signerText(s *registry.Signer) string {
	if s == nil {
		return "none (unsigned)"
	}
	return s.String()
}

// errPinDropped reports that trust accept removed the pin of a package it
// cannot resolve, instead of pinning a new signer.
var errPinDropped = errors.New("signer pin removed")

// registryTrustAccept re-resolves one package with its signer pin waived and
// returns the signer the lock pinned before and after. For a package that is
// only pinned, it removes the pin and returns errPinDropped.
func registryTrustAccept(ctx context.Context, p registryProject, arg string) (name string, before, after *registry.Signer, res mcp.RegistryMutationResult, err error) {
	spec, err := registry.ParseSpec(arg)
	if err != nil {
		return "", nil, nil, res, ConfigError(err)
	}
	lock, err := registry.LoadLockfile(p.Root)
	if err != nil {
		return "", nil, nil, res, ConfigError(err)
	}
	_, declared := p.Cfg.Dependencies[spec.Name]
	old := lock.Get(spec.Name)
	before = lock.Pin(spec.Name)
	if old == nil && !declared {
		if before == nil {
			return "", nil, nil, res, ConfigErrorf("%s is neither declared in bino.toml nor locked or pinned in %s", spec.Name, registry.LockfileName)
		}
		if spec.Ref != "" {
			return "", nil, nil, res, ConfigErrorf("%s is only pinned: accept it without a version to remove the pin", spec.Name)
		}
		if err := lock.CheckWritable(); err != nil {
			return "", nil, nil, res, ConfigError(err)
		}
		lock.SetPin(spec.Name, nil)
		if err := registry.SaveLockfile(p.Root, lock); err != nil {
			return "", nil, nil, res, RuntimeError(err)
		}
		return spec.Name, before, nil, res, errPinDropped
	}
	accept := map[string]bool{spec.Name: true}
	switch {
	case declared && spec.Ref != "":
		res, err = registryAdd(ctx, p, []string{arg}, accept)
	case declared:
		res, err = registryUpdate(ctx, p, []string{spec.Name}, accept)
	case spec.Ref != "":
		return "", nil, nil, res, ConfigErrorf("%s is a transitive dependency: accept it without a version; it is resolved through the packages that depend on it", spec.Name)
	default:
		parents := directDependents(lock, spec.Name)
		if len(parents) == 0 {
			return "", nil, nil, res, ConfigErrorf("no declared dependency of this project pulls in %s - run 'bino registry update'", spec.Name)
		}
		res, err = registryUpdate(ctx, p, parents, accept)
	}
	if err != nil {
		return "", nil, nil, res, err
	}
	lock, err = registry.LoadLockfile(p.Root)
	if err != nil {
		return "", nil, nil, res, ConfigError(err)
	}
	return spec.Name, before, lock.Pin(spec.Name), res, nil
}

// directDependents returns the direct dependencies whose locked closure
// contains name.
func directDependents(lock *registry.Lockfile, name string) []string {
	var out []string
	for _, e := range lock.Packages {
		if !e.Direct {
			continue
		}
		seen := map[string]bool{}
		stack := append([]string(nil), e.Dependencies...)
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n == name {
				out = append(out, e.Name)
				break
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			if d := lock.Get(n); d != nil {
				stack = append(stack, d.Dependencies...)
			}
		}
	}
	sort.Strings(out)
	return out
}
