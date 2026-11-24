package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// attestationsDir holds the verified attestations of locked packages, so
// 'bino registry verify' can re-check signatures offline. A scope cannot
// start with a dot, so the directory never collides with a package, and the
// loader reads only YAML, so the build never sees it.
const attestationsDir = ".attestations"

// storedAttestations is the on-disk form: the version the bundles were
// verified for, and the bundles themselves.
type storedAttestations struct {
	Version      string        `json:"version"`
	Attestations []Attestation `json:"attestations"`
}

// AttestationsPath returns where a package's attestations are kept:
// .bino/registry/.attestations/<scope>/<name>.json.
func AttestationsPath(projectRoot, name string) (abs string, err error) {
	scope, base, err := ParseName(name)
	if err != nil {
		return "", err
	}
	return storeContain(projectRoot, path.Join(".bino", "registry", attestationsDir, scope, base+".json"))
}

// WriteAttestations stores the attestations of a package version. None
// removes any earlier file, so a package that became unsigned keeps none.
func WriteAttestations(projectRoot, name, version string, atts []Attestation) error {
	if len(atts) == 0 {
		return RemoveAttestations(projectRoot, name)
	}
	abs, err := AttestationsPath(projectRoot, name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(storedAttestations{Version: version, Attestations: atts})
	if err != nil {
		return err
	}
	if err := mkdirAllContained(projectRoot, filepath.Dir(abs)); err != nil {
		return err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	return nil
}

// ReadAttestations returns the stored attestations of a package, and the
// version they were stored for. A missing file is not an error.
func ReadAttestations(projectRoot, name string) (version string, atts []Attestation, err error) {
	abs, err := AttestationsPath(projectRoot, name)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var s storedAttestations
	if err := json.Unmarshal(data, &s); err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", abs, err)
	}
	return s.Version, s.Attestations, nil
}

// RemoveAttestations deletes a package's stored attestations, if any.
func RemoveAttestations(projectRoot, name string) error {
	abs, err := AttestationsPath(projectRoot, name)
	if err != nil {
		return err
	}
	if err := verifyContainedAfterResolve(projectRoot, filepath.Dir(abs)); err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", abs, err)
	}
	pruneEmptyParents(filepath.Dir(abs), filepath.Join(StoreDir(projectRoot), attestationsDir))
	return nil
}

// StoredSignatures verifies the attestations stored for a locked package
// offline, at the times the lock records. found is false when none are
// stored for the locked version. A manifest that cannot be digested is
// ErrNotCanonical.
func StoredSignatures(projectRoot string, e Entry, policy *TrustPolicy) (check SignatureCheck, found bool, err error) {
	version, atts, err := ReadAttestations(projectRoot, e.Name)
	if err != nil {
		return check, false, err
	}
	if version != e.Version || len(atts) == 0 || !e.IsTree() {
		return check, false, nil // stored for another version: as good as none
	}
	digest, _, err := ManifestDigest(e.Files)
	if err != nil {
		return check, false, err
	}
	check, err = VerifyAttestations(e.Name, e.Version, digest, atts, policy, e.VerifiedAt)
	return check, err == nil, err
}
