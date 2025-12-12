package config

import (
	"encoding/json"
	"fmt"
)

// SigningProfile captures certificate, key, and signer metadata used to sign PDFs.
type SigningProfile struct {
	Document Document
	Spec     SigningProfileSpec
}

// SigningProfileSpec mirrors the SigningProfile manifest spec.
type SigningProfileSpec struct {
	Certificate     PEMSource            `json:"certificate"`
	PrivateKey      PEMSource            `json:"privateKey"`
	TSAURL          string               `json:"tsaURL"`
	DigestAlgorithm string               `json:"digestAlgorithm"`
	CertType        string               `json:"certType"`
	DocMDPPerm      string               `json:"docMDPPerm"`
	Signer          SigningProfileSigner `json:"signer"`
}

// SigningProfileSigner holds signer identity metadata.
type SigningProfileSigner struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Reason   string `json:"reason"`
	Contact  string `json:"contact"`
}

// PEMSource represents either inline PEM content or a filesystem path to PEM data.
type PEMSource struct {
	Inline string `json:"inline"`
	Path   string `json:"path"`
}

// CollectSigningProfiles parses SigningProfile manifests into name-addressable structs.
func CollectSigningProfiles(docs []Document) (map[string]SigningProfile, error) {
	profiles := make(map[string]SigningProfile)
	for _, doc := range docs {
		if doc.Kind != "SigningProfile" {
			continue
		}
		if doc.Name == "" {
			return nil, fmt.Errorf("signing profile missing metadata.name")
		}
		if _, exists := profiles[doc.Name]; exists {
			return nil, fmt.Errorf("multiple SigningProfile documents share metadata.name %q", doc.Name)
		}
		var payload struct {
			Spec SigningProfileSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse SigningProfile %s: %w", doc.Name, err)
		}
		if payload.Spec.Signer.Name == "" {
			return nil, fmt.Errorf("SigningProfile %s: spec.signer.name is required", doc.Name)
		}
		profiles[doc.Name] = SigningProfile{Document: doc, Spec: payload.Spec}
	}
	return profiles, nil
}
